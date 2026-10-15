package store

import "testing"

// Fixed samples: one key, one passphrase, one plaintext, and the two
// ciphertexts they produce. They are pinned here so a broken key derivation
// or ciphertext format fails here first — not the day production cannot read
// its config.
const (
	sampleFernetKey  = "a9Abfm3N7fV2A1xsrdfE68FhvoOVlKhJJrw7iUIuNXw="
	samplePassphrase = "hunter2"
	samplePlaintext  = "sk-live-测试-<&>-42"

	sampleTokenFromKey        = "enc:v1:gAAAAABqqr0bEl9oouEHmFd7193Wcz2MIkcLsKiv5N2_Iv_IjKcJJ8UhCZOejjZm_mjgchyAm07JVWIeM4ftth8SCOKtKySP8NMJxRt62a5xg5Uwvi-hhwY="
	sampleTokenFromPassphrase = "enc:v1:gAAAAABqqr0coNlEIANJ2XdSOo5wCF6r9chw3ateYWLkk6OBVv5lkalRWQz94k_bH0a35AelWYpbbOjecR4MIS4gzQCYoQTBVFA4BVE9M0-Nq2qHHe5l1W4="
)

func TestDecryptExistingCiphertext(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		token string
	}{
		{"密钥本身就是合法 Fernet key", sampleFernetKey, sampleTokenFromKey},
		{"密钥是口令，取 SHA-256", samplePassphrase, sampleTokenFromPassphrase},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newCipher(tc.key).decrypt(tc.token); got != samplePlaintext {
				t.Errorf("解既有密文得到 %q, 期望 %q", got, samplePlaintext)
			}
		})
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"ASCII 密钥串", sampleFernetKey, "sk-abcdef0123456789"},
		{"中文与 HTML 字符", sampleFernetKey, "密钥-<&>-\"quoted\""},
		{"口令派生密钥", samplePassphrase, "another-secret"},
		{"中文口令", "短口令中文", "another-secret"},
		{"64 位十六进制按口令处理", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "hex-key-secret"},
		{"少了补位符的 43 字符按口令处理", "a9Abfm3N7fV2A1xsrdfE68FhvoOVlKhJJrw7iUIuNXw", "unpadded-secret"},
		{"掺了非字母表字符仍算合法密钥", "a9Abfm3N7fV2A1xsrd!fE68FhvoOVlKhJJrw7iUIuNXw=", "lenient-secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCipher(tc.key)
			token := c.encrypt(tc.value)
			if !isEncrypted(token) {
				t.Fatalf("密文没有 %s 前缀: %q", encryptedPrefix, token)
			}
			if token == tc.value {
				t.Fatal("密文与明文相同，等于没加密")
			}
			if got := c.decrypt(token); got != tc.value {
				t.Errorf("往返后得到 %q, 期望 %q", got, tc.value)
			}
		})
	}
}

// A 43-character unpadded string must take the passphrase branch: base64
// decoding without padding fails, so it is treated as a passphrase, not a key.
// This check decides the derived key; change it and existing ciphertext stays
// locked.
func TestKeyNormalization(t *testing.T) {
	padded := newCipher(sampleFernetKey)
	unpadded := newCipher("a9Abfm3N7fV2A1xsrdfE68FhvoOVlKhJJrw7iUIuNXw")
	if got := unpadded.decrypt(padded.encrypt("x")); got == "x" {
		t.Error("去掉补位符后应当推出另一个密钥，却解开了同一条密文")
	}

	// characters outside the alphabet are ignored, so this is equivalent to the original key
	lenient := newCipher("a9Abfm3N7fV2A1xsrd!fE68FhvoOVlKhJJrw7iUIuNXw=")
	if got := lenient.decrypt(padded.encrypt("x")); got != "x" {
		t.Errorf("忽略非字母表字符后应当是同一个密钥，却得到 %q", got)
	}
}

func TestEncryptWithoutKeyIsPassThrough(t *testing.T) {
	c := newCipher("")
	if c.enabled() {
		t.Fatal("没设密钥时不该启用加密")
	}
	const secret = "plain-api-key"
	if got := c.encrypt(secret); got != secret {
		t.Errorf("加密得到 %q, 期望原样返回 %q", got, secret)
	}
	if got := c.decrypt(secret); got != secret {
		t.Errorf("解密得到 %q, 期望原样返回 %q", got, secret)
	}
	// without a key, ciphertext passes through as-is — still better than failing to list the whole config
	if got := c.decrypt(sampleTokenFromKey); got != sampleTokenFromKey {
		t.Errorf("没有密钥时解密应当原样返回，却得到 %q", got)
	}
}

func TestEncryptIsIdempotent(t *testing.T) {
	c := newCipher(sampleFernetKey)
	once := c.encrypt("secret")
	if twice := c.encrypt(once); twice != once {
		t.Error("对密文再加密不该套上第二层")
	}
	if got := c.encrypt(""); got != "" {
		t.Errorf("空值应当原样返回，却得到 %q", got)
	}
}

func TestDecryptGarbageReturnsInput(t *testing.T) {
	c := newCipher(sampleFernetKey)
	const broken = encryptedPrefix + "this-is-not-a-fernet-token"
	if got := c.decrypt(broken); got != broken {
		t.Errorf("解不开时应当原样返回，却得到 %q", got)
	}
	// a different key cannot decrypt either; return as-is rather than erroring
	other := newCipher("完全不同的口令")
	token := c.encrypt("secret")
	if got := other.decrypt(token); got != token {
		t.Errorf("换密钥后应当原样返回，却得到 %q", got)
	}
}
