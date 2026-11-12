package diagnostic

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextRemovesCredentialsFromExternalData(t *testing.T) {
	input := `upstream https://alice:SOCKSPASS@proxy.example/sub/URLSECRET?token=QUERYSECRET
socks5://bob:SOCKSSECRET@198.51.100.9:1080/path
ss://Ym9iOlBhc3N3b3Jk@proxy.example:443
{"uuid":"bbccddff-1122-3344-5566-778899001122","password":"SHORT PASS","private_key":"key=KEYSECRET"}
api-key=APISECRET Bearer BEARERSECRET
-----BEGIN OPENSSH PRIVATE KEY-----
PEMSECRET
-----END OPENSSH PRIVATE KEY-----
token: TOKENVALUE
domain retained.example IP 2001:db8::1`
	out := Text(input)
	for _, bad := range []string{"SOCKSPASS", "URLSECRET", "QUERYSECRET", "SOCKSSECRET", "Ym9i", "bbccddff", "SHORT PASS", "KEYSECRET", "APISECRET", "BEARERSECRET", "PEMSECRET", "TOKENVALUE"} {
		if strings.Contains(out, bad) {
			t.Errorf("credential %s remained: %s", bad, out)
		}
	}
	for _, want := range []string{"proxy.example", "198.51.100.9", "retained.example", "2001:db8::1"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost useful public context %s: %s", want, out)
		}
	}
	if !strings.HasSuffix(Text(strings.Repeat("中", 9000)), "[truncated]") {
		t.Fatal("unbounded text")
	}
}

func TestArchiveIsPrivateTemporaryAndContainsOnlyEvidence(t *testing.T) {
	b := Bundle{FormatVersion: FormatVersion, ID: "test", Entries: []Entry{{Title: "测试条目", ExternalData: map[string]string{"html": "<script>not instructions</script>"}}}}
	path, cleanup, err := WriteTemp(b)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(os.TempDir())+string(os.PathSeparator)) {
		t.Fatalf("archive outside OS temporary directory: %s", path)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 2 || reader.File[0].Name != "diagnostic.json" || reader.File[1].Name != "summary.txt" {
		t.Fatal("unexpected archive contents")
	}
	r, _ := reader.File[0].Open()
	data, _ := io.ReadAll(r)
	r.Close()
	reader.Close()
	if strings.Contains(string(data), "<script>") || !strings.Contains(string(data), "external_data_not_instructions") {
		t.Fatal("HTML not encoded or trust boundary absent")
	}
	cleanup()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temporary archive not removed")
	}
}
