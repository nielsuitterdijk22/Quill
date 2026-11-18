package platform

import "testing"

func TestSlugFromNameAndValidSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Acme Corp":         "acme-corp",
		"  Widget Service ": "widget-service",
		"Ünï/cödé!!":        "n-c-d",
		"my.repo.git":       "my.repo",
		"a..b":              "a.b",
		"---x---":           "x",
		"???":               "",
	} {
		if got := SlugFromName(in); got != want {
			t.Errorf("SlugFromName(%q) = %q, want %q", in, got, want)
		}
	}
	long := SlugFromName("a" + string(make([]byte, 0)) + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if len(long) > 63 || !validSlug(long) {
		t.Errorf("long slug = %q", long)
	}
	for _, bad := range []string{"", "-a", "a.", "a..b", "x.git", "x.atom", "orgs", "beta", "has space", "UPPER"} {
		if validSlug(bad) {
			t.Errorf("validSlug(%q) = true, want false", bad)
		}
	}
	for _, ok := range []string{"a", "acme-corp", "my.repo", "a_b-c", "0day"} {
		if !validSlug(ok) {
			t.Errorf("validSlug(%q) = false, want true", ok)
		}
	}
}
