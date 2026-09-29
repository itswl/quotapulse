package notify

import "strings"

// Implementation note.
// Implementation note.

type feishuTextPayload struct {
	MsgType string            `json:"msg_type"`
	Content feishuTextContent `json:"content"`
}

type feishuTextContent struct {
	Text string `json:"text"`
}

type feishuCardPayload struct {
	MsgType string     `json:"msg_type"`
	Card    feishuCard `json:"card"`
}

type feishuCard struct {
	Header   feishuCardHeader    `json:"header"`
	Elements []feishuCardElement `json:"elements"`
}

type feishuCardHeader struct {
	Title    feishuCardTitle `json:"title"`
	Template string          `json:"template"`
}

type feishuCardTitle struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type feishuCardElement struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type dingtalkPayload struct {
	MsgType  string           `json:"msgtype"`
	Markdown dingtalkMarkdown `json:"markdown"`
}

type dingtalkMarkdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type wecomTextPayload struct {
	MsgType string           `json:"msgtype"`
	Text    wecomTextContent `json:"text"`
}

type wecomTextContent struct {
	Content string `json:"content"`
}

type wecomMarkdownPayload struct {
	MsgType  string        `json:"msgtype"`
	Markdown wecomMarkdown `json:"markdown"`
}

type wecomMarkdown struct {
	Content string `json:"content"`
}

// Implementation note.
type customPayload struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
}

// Implementation note.
// Implementation note.
// Implementation note.
type envelope struct {
	Type      string `json:"Type"`
	RuleName  string `json:"RuleName"`
	Level     string `json:"Level"`
	Resources []any  `json:"Resources"`
}

type balanceResource struct {
	ProjectName  string  `json:"ProjectName"`
	OwnerProject *string `json:"OwnerProject"`
	Provider     string  `json:"Provider"`
	BalanceType  string  `json:"BalanceType"`
	CurrentValue float64 `json:"CurrentValue"`
	Threshold    float64 `json:"Threshold"`
	Unit         string  `json:"Unit"`
	Message      string  `json:"Message"`
}

type checkFailedResource struct {
	ProjectName  string  `json:"ProjectName"`
	OwnerProject *string `json:"OwnerProject"`
	Provider     string  `json:"Provider"`
	Error        string  `json:"Error"`
	Message      string  `json:"Message"`
}

type subscriptionResource struct {
	SubscriptionName string  `json:"SubscriptionName"`
	OwnerProject     *string `json:"OwnerProject"`
	RenewalDay       int     `json:"RenewalDay"`
	CycleType        string  `json:"CycleType"`
	DaysUntilRenewal int     `json:"DaysUntilRenewal"`
	Amount           float64 `json:"Amount"`
	Message          string  `json:"Message"`
}

// Implementation note.
//
// Implementation note.
// Implementation note.
func (n *notifier) payload(msg Message) any {
	text := strings.Join(msg.Lines, "\n")
	if msg.Kind == KindBalance || msg.Kind == KindSubscription {
		return n.plainPayload(msg.Title, text, msg.envelope)
	}
	return n.richPayload(msg.Title, text)
}

func (n *notifier) plainPayload(title, text string, env *envelope) any {
	switch n.typ {
	case TypeFeishu:
		return feishuTextPayload{
			MsgType: "text",
			Content: feishuTextContent{Text: "[" + title + "]\n\n" + text + "\nSource: " + n.source},
		}
	case TypeDingTalk:
		return dingtalkPayload{
			MsgType:  "markdown",
			Markdown: dingtalkMarkdown{Title: title, Text: "## " + title + "\n\n" + dingtalkList(text)},
		}
	case TypeCustom:
		if env != nil {
			return env
		}
		// Implementation note.
		return n.customPayload(title, text)
	default: // wecom
		return wecomTextPayload{
			MsgType: "text",
			Text:    wecomTextContent{Content: "[" + title + "]\n" + text},
		}
	}
}

func (n *notifier) richPayload(title, content string) any {
	switch n.typ {
	case TypeFeishu:
		return feishuCardPayload{
			MsgType: "interactive",
			Card: feishuCard{
				Header: feishuCardHeader{
					Title:    feishuCardTitle{Tag: "plain_text", Content: title},
					Template: "orange",
				},
				Elements: []feishuCardElement{{Tag: "markdown", Content: content}},
			},
		}
	case TypeDingTalk:
		return dingtalkPayload{
			MsgType:  "markdown",
			Markdown: dingtalkMarkdown{Title: title, Text: "### " + title + "\n\n" + content},
		}
	case TypeWeCom:
		return wecomMarkdownPayload{
			MsgType:  "markdown",
			Markdown: wecomMarkdown{Content: "### " + title + "\n\n" + content},
		}
	default: // custom
		return n.customPayload(title, content)
	}
}

func (n *notifier) customPayload(title, content string) customPayload {
	return customPayload{Title: title, Content: content, Source: n.source, Timestamp: isoLocal(n.now())}
}

// Implementation note.
// Implementation note.
func dingtalkList(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if key, value, found := strings.Cut(line, ": "); found {
			out = append(out, "- **"+key+"**: "+value)
			continue
		}
		out = append(out, "- "+line)
	}
	return strings.Join(out, "\n")
}
