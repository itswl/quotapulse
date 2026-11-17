// System: is the thing alive, and getting everything out of it and back in —
// a backup of the database, an export by name, an import that merges.
import { DatabaseBackup, Download, FileUp, RefreshCw, Upload } from 'lucide-preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { backup, exportURL, importBundle } from '../../api/endpoints';
import type { ImportReport, Job, SystemInfo } from '../../api/types';
import { Banner, Button, Card, Chip, ErrorState, Field, Skeleton, StatusDot, Switch, type Status } from '../../components';
import { errorText } from '../../errors';
import { formatAgo, formatDateTime, formatNumber, formatSpan, toMs } from '../../format';
import { useAction } from '../../hooks';
import { lang, t, translate } from '../../i18n';
import { now } from '../../state/clock';
import { providers } from '../../state/providers';
import { useResource } from '../../state/resource';
import { checkSession } from '../../state/session';
import { settings } from '../../state/settings';
import { formatBytes } from './logic';
import { Block, CheckBox, Fact, Facts } from './parts';
import { channels, system, tokens } from './state';

const REFRESH_MS = 15_000;

export function SystemSection() {
  useResource(system);
  // Nothing on the stream says "a loop ran": ask again every so often, so
  // the page shows the loops turning.
  useEffect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') void system.load();
    }, REFRESH_MS);
    return () => clearInterval(timer);
  }, []);

  const info = system.data.value;
  return (
    <div class="st-blocks">
      {!info ? (
        system.error.value ? (
          <ErrorState compact error={system.error.value} onRetry={() => void system.load()} />
        ) : (
          <div aria-busy="true">
            <span class="sr-only">{t('common.state.loading')}</span>
            <Skeleton height="220px" />
          </div>
        )
      ) : (
        <>
          <About info={info} />
          <Loops info={info} />
        </>
      )}
      <BackupBlock />
      <ExportBlock />
      <ImportBlock />
    </div>
  );
}

// ---------- about ----------

function About({ info }: { info: SystemInfo }) {
  const { stats } = info;
  const counts: [string, number][] = [
    [t('settings.system.count.accounts'), stats.accounts],
    [t('settings.system.count.meters'), stats.meters],
    [t('settings.system.count.readings'), stats.readings],
    [t('settings.system.count.reminders'), stats.reminders],
    [t('settings.system.count.incidents'), stats.incidents],
    [t('settings.system.count.deliveries'), stats.deliveries],
  ];
  return (
    <Block title={t('settings.system.about.title')} about={t('settings.system.about.about')}>
      <Card>
        <Facts class="st-facts--two">
          <Fact label={t('settings.system.version')}>
            <span class="num">{info.version}</span>
            {info.demo && (
              <Chip tone="info" class="st-facts__chip">
                {t('common.shell.demo')}
              </Chip>
            )}
          </Fact>
          <Fact label={t('settings.system.size')}>
            <span class="num">{formatBytes(stats.bytes)}</span>
          </Fact>
          {counts.map(([label, value]) => (
            <Fact key={label} label={label}>
              <span class="num">{formatNumber(value, 0)}</span>
            </Fact>
          ))}
        </Facts>
        {info.muted && <p class="st-note st-note--gap">{t('common.shell.muted')}</p>}
      </Card>
    </Block>
  );
}

// ---------- the background loops ----------

function known(key: string): string | undefined {
  const text = translate(lang.value, key);
  return text === key ? undefined : text;
}

/** How a loop stands. Late is the server's own measure (runtime.Healthy):
 *  ten missed turns and two minutes. */
function jobState(job: Job, at: number): { status: Status; label: string } {
  if (job.last_run === null) return { status: 'pending', label: t('settings.system.job.notYet') };
  if (job.last_error) return { status: 'warning', label: t('settings.system.job.failed') };
  if (at - toMs(job.last_run) > 10 * job.every_seconds * 1000 + 120_000) return { status: 'warning', label: t('settings.system.job.late') };
  return { status: 'ok', label: t('settings.system.job.ok') };
}

function every(seconds: number): string {
  return seconds < 60 ? t('settings.system.job.seconds', { n: seconds }) : formatSpan(seconds * 1000);
}

function Loops({ info }: { info: SystemInfo }) {
  const at = now.value;
  return (
    <Block title={t('settings.system.jobs.title')} about={t('settings.system.jobs.about')}>
      <Card flush>
        {info.jobs.length === 0 ? (
          <p class="st-note st-note--pad">{t('settings.system.jobs.none')}</p>
        ) : (
          <ul class="st-jobs">
            {info.jobs.map((job) => {
              const state = jobState(job, at);
              return (
                <li key={job.name} class="st-job">
                  <div class="st-job__text">
                    <p class="st-job__name">{known(`settings.system.loop.${job.name}`) ?? job.name}</p>
                    <p class="st-job__about">{known(`settings.system.loop.${job.name}.about`) ?? ''}</p>
                    {job.last_error && <p class="st-item__error">{job.last_error}</p>}
                  </div>
                  <div class="st-job__state">
                    <StatusDot status={state.status} label={state.label} />
                    <p class="st-job__meta">
                      {job.last_run !== null && (
                        <time dateTime={job.last_run} title={formatDateTime(job.last_run)}>
                          {t('settings.system.job.lastRun', { ago: formatAgo(job.last_run) })}
                        </time>
                      )}
                      <span>{t('settings.system.job.every', { span: every(job.every_seconds) })}</span>
                      <span>{t('settings.system.job.runs', { n: job.runs, count: formatNumber(job.runs, 0) })}</span>
                    </p>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        <p class="st-jobs__foot">
          <span>{t('settings.system.jobs.asOf', { time: formatDateTime(info.now) })}</span>
          <Button variant="ghost" size="sm" icon={RefreshCw} busy={system.loading.value} onClick={() => void system.load()}>
            {t('settings.system.jobs.refresh')}
          </Button>
        </p>
      </Card>
    </Block>
  );
}

// ---------- backup ----------

function BackupBlock() {
  const [path, setPath] = useState<string | undefined>(undefined);
  const run = useAction(async () => {
    setPath(undefined);
    setPath((await backup()).path);
  });
  return (
    <Block title={t('settings.system.backup.title')} about={t('settings.system.backup.about')}>
      <Card class="st-action">
        <Button icon={DatabaseBackup} busy={run.busy} onClick={() => void run.run()}>
          {t('settings.system.backup.now')}
        </Button>
        {run.error !== undefined && (
          <p class="form-error" role="alert">
            {errorText(run.error)}
          </p>
        )}
        {path !== undefined &&
          (path === '' ? (
            <p class="st-note" role="status">
              {t('settings.system.backup.nowhere')}
            </p>
          ) : (
            <p class="st-result" role="status">
              {t('settings.system.backup.wrote')} <code class="st-path">{path}</code>
            </p>
          ))}
      </Card>
    </Block>
  );
}

// ---------- export ----------

/** Fetches the export with the session and hands it to the browser as a
 *  download. A link would do the first half, but could only show a failure
 *  as a page of JSON. */
async function download(options: { secrets: boolean; history: boolean }): Promise<{ name: string; bytes: number }> {
  let response: Response;
  try {
    response = await fetch(exportURL(options), { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } });
  } catch {
    throw new ApiError(0, 'network', 'the server could not be reached');
  }
  if (!response.ok) {
    const body = (await response.json().catch(() => undefined)) as { error?: { code?: string; message?: string } } | undefined;
    // The session ended: let the app find out, and show the sign-in screen.
    if (response.status === 401) void checkSession();
    throw new ApiError(response.status, body?.error?.code ?? (response.status === 401 ? 'unauthorized' : 'internal'), body?.error?.message ?? response.statusText);
  }
  const blob = await response.blob();
  const name = /filename="?([^";]+)"?/.exec(response.headers.get('Content-Disposition') ?? '')?.[1] ?? 'quotapulse.json';
  const href = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = href;
  link.download = name;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(href), 30_000);
  return { name, bytes: blob.size };
}

function ExportBlock() {
  const [secrets, setSecrets] = useState(false);
  const [history, setHistory] = useState(true);
  const [saved, setSaved] = useState<{ name: string; bytes: number; secrets: boolean } | undefined>(undefined);
  const run = useAction(async () => {
    setSaved(undefined);
    setSaved({ ...(await download({ secrets, history })), secrets });
  });
  return (
    <Block title={t('settings.system.export.title')} about={t('settings.system.export.about')}>
      <Card class="st-action">
        <Field class="st-switch" label={t('settings.system.export.secrets')} help={t('settings.system.export.secrets.help')}>
          <Switch checked={secrets} onChange={setSecrets} />
        </Field>
        {secrets && <Banner tone="warning">{t('settings.system.export.secrets.warning')}</Banner>}
        <Field class="st-switch" label={t('settings.system.export.history')} help={t('settings.system.export.history.help')}>
          <Switch checked={history} onChange={setHistory} />
        </Field>
        <div class="st-action__row">
          <Button icon={Download} busy={run.busy} onClick={() => void run.run()}>
            {t('settings.system.export.download')}
          </Button>
          {saved && (
            <p class="st-result" role="status">
              {t(saved.secrets ? 'settings.system.export.savedSecrets' : 'settings.system.export.saved', { name: saved.name, size: formatBytes(saved.bytes) })}
            </p>
          )}
        </div>
        {run.error !== undefined && (
          <p class="form-error" role="alert">
            {errorText(run.error)}
          </p>
        )}
      </Card>
    </Block>
  );
}

// ---------- import ----------

const KINDS = ['providers', 'channels', 'accounts', 'meters', 'reminders', 'tokens', 'incidents'];

/** The kinds of thing a report counts, in a steady order, unknown ones last. */
function counted(counts: Record<string, number>): [string, number][] {
  const names = [...KINDS.filter((kind) => kind in counts), ...Object.keys(counts).filter((kind) => !KINDS.includes(kind))];
  return names.map((kind) => [known(`settings.system.import.kind.${kind}`) ?? kind, counts[kind] ?? 0]);
}

function ImportBlock() {
  const input = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [withSettings, setWithSettings] = useState(false);
  const [report, setReport] = useState<ImportReport | undefined>(undefined);
  const [problem, setProblem] = useState<string | undefined>(undefined);
  const [applied, setApplied] = useState(false);

  const run = useAction(async () => {
    if (!file) return;
    setReport(undefined);
    setProblem(undefined);
    let bundle: unknown;
    try {
      bundle = JSON.parse(await file.text());
    } catch {
      setProblem(t('settings.system.import.notJson', { name: file.name }));
      return;
    }
    try {
      setReport(await importBundle(bundle, { settings: withSettings }));
      setApplied(withSettings && typeof bundle === 'object' && bundle !== null && 'settings' in bundle);
    } catch (cause) {
      setProblem(errorText(cause));
      return;
    }
    // Anything may have been added: what is on screen elsewhere is stale.
    for (const each of [system, channels, tokens, settings, providers]) void each.load();
  });

  const pick = (chosen: File | null) => {
    setFile(chosen);
    setReport(undefined);
    setProblem(undefined);
  };

  return (
    <Block title={t('settings.system.import.title')} about={t('settings.system.import.about')}>
      <Card class="st-action">
        <div class="st-file">
          <input
            ref={input}
            class="sr-only"
            type="file"
            accept=".json,application/json"
            aria-label={t('settings.system.import.choose')}
            tabIndex={-1}
            onChange={(event) => pick(event.currentTarget.files?.[0] ?? null)}
          />
          <Button
            icon={FileUp}
            disabled={run.busy}
            onClick={() => {
              // Emptied first, so choosing the same file again still counts as a choice.
              if (input.current) input.current.value = '';
              input.current?.click();
            }}
          >
            {file ? t('settings.system.import.another') : t('settings.system.import.choose')}
          </Button>
          <span class="st-file__name">{file ? `${file.name} · ${formatBytes(file.size)}` : t('settings.system.import.noFile')}</span>
        </div>

        <CheckBox checked={withSettings} onChange={setWithSettings} label={t('settings.system.import.settings')} help={t('settings.system.import.settings.help')} />

        <div class="st-action__row">
          <Button variant="primary" icon={Upload} busy={run.busy} disabled={!file} onClick={() => void run.run()}>
            {t('settings.system.import.run')}
          </Button>
        </div>

        {problem !== undefined && (
          <p class="form-error" role="alert">
            {problem}
          </p>
        )}
        {report && <Report report={report} settingsApplied={applied} />}
      </Card>
    </Block>
  );
}

function Report({ report, settingsApplied }: { report: ImportReport; settingsApplied: boolean }) {
  const created = counted(report.created ?? {});
  const skipped = counted(report.skipped ?? {});
  const notes = report.notes ?? [];
  const total = (rows: [string, number][]) => rows.reduce((sum, [, n]) => sum + n, 0);
  const list = (rows: [string, number][]) => (
    <ul class="st-report__counts">
      {rows.map(([kind, n]) => (
        <li key={kind}>
          {kind} <span class="num">{formatNumber(n, 0)}</span>
        </li>
      ))}
    </ul>
  );
  return (
    <div class="st-report" role="status">
      <p class="st-report__title">{created.length + skipped.length === 0 ? t('settings.system.import.nothing') : t('settings.system.import.done', { created: total(created), skipped: total(skipped) })}</p>
      {settingsApplied && <p>{t('settings.system.import.settingsApplied')}</p>}
      {created.length > 0 && (
        <div class="st-report__part">
          <h3>{t('settings.system.import.created')}</h3>
          {list(created)}
        </div>
      )}
      {skipped.length > 0 && (
        <div class="st-report__part">
          <h3>{t('settings.system.import.skipped')}</h3>
          {list(skipped)}
        </div>
      )}
      {notes.length > 0 && (
        <details class="st-report__notes" open={notes.length <= 6}>
          <summary>{t('settings.system.import.notes', { n: notes.length })}</summary>
          <ul>
            {notes.map((note, index) => (
              <li key={index}>{note}</li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
