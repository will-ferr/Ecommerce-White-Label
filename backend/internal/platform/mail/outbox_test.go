package mail

import "testing"

func TestRedactRemovesAddresses(t *testing.T) {
	got := redact(`550 5.1.1 <maria.silva+loja@exemplo.com.br>: Recipient address rejected; from=ana@loja.test`)
	want := `550 5.1.1 <[e-mail]>: Recipient address rejected; from=[e-mail]`
	if got != want {
		t.Errorf("redact() = %q, want %q", got, want)
	}
}
