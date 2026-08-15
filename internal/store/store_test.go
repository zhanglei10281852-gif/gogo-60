package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"CaveLoop/internal/model"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	handle, err := Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("Open returned %v", err)
	}
	return handle
}

func sampleRecords() []model.Record {
	instrument := model.Instrument{ID: "set-a"}
	trip := model.Trip{ID: "T1", Shots: []model.Shot{{ID: "S1", From: "A", To: "B", Distance: 5}}}
	return []model.Record{
		{Kind: model.KindInstrument, Cave: "Store Cave", Instrument: &instrument},
		{Kind: model.KindTrip, Cave: "Store Cave", Trip: &trip},
	}
}

func TestOpenCreatesDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "store")
	handle, err := Open(root)
	if err != nil {
		t.Fatalf("Open returned %v", err)
	}
	info, err := os.Stat(handle.Root())
	if err != nil || !info.IsDir() {
		t.Fatalf("the store directory was not created: %v", err)
	}
	if !filepath.IsAbs(handle.Root()) {
		t.Fatalf("store root %q is not absolute", handle.Root())
	}
	if _, err := Open("  "); err == nil {
		t.Fatal("an empty directory name was accepted")
	}
}

func TestAppendRecordsAndReload(t *testing.T) {
	handle := openTemp(t)
	if handle.HasLedger() {
		t.Fatal("a fresh store should have no ledger")
	}
	report, err := handle.AppendRecords(sampleRecords())
	if err != nil {
		t.Fatalf("AppendRecords returned %v", err)
	}
	if report.Appended != 2 || report.TotalRecords != 2 {
		t.Fatalf("append report is %+v", report)
	}
	if report.LedgerDigest == "" || report.PayloadDigest == "" {
		t.Fatalf("digests are missing from %+v", report)
	}
	if !handle.HasLedger() {
		t.Fatal("the ledger was not created")
	}
	second, err := handle.AppendRecords(sampleRecords()[1:])
	if err != nil {
		t.Fatalf("second AppendRecords returned %v", err)
	}
	if second.TotalRecords != 3 {
		t.Fatalf("the ledger holds %d records", second.TotalRecords)
	}
	survey, err := handle.LoadSurvey()
	if err != nil {
		t.Fatalf("LoadSurvey returned %v", err)
	}
	if survey.Cave != "Store Cave" || len(survey.Trips) != 1 || len(survey.Instruments) != 1 {
		t.Fatalf("the folded survey is %+v", survey)
	}
	if _, err := handle.AppendRecords(nil); err == nil {
		t.Fatal("appending nothing was accepted")
	}
}

func TestLoadSurveyOnEmptyStore(t *testing.T) {
	handle := openTemp(t)
	records, err := handle.LoadRecords()
	if err != nil || records != nil {
		t.Fatalf("LoadRecords on an empty store returned %v, %v", records, err)
	}
	if _, err := handle.LoadSurvey(); err == nil {
		t.Fatal("LoadSurvey on an empty store should fail")
	}
}

func TestLoadRecordsRejectsCorruptLedger(t *testing.T) {
	handle := openTemp(t)
	if err := os.WriteFile(handle.LedgerPath(), []byte("{\"kind\":\"trip\"}\n"), 0o600); err != nil {
		t.Fatalf("writing ledger: %v", err)
	}
	if _, err := handle.LoadRecords(); err == nil {
		t.Fatal("a trip record without payload was accepted")
	}
}

func TestWriteAndReadJSONAtomically(t *testing.T) {
	handle := openTemp(t)
	type payload struct {
		Name string `json:"name"`
	}
	digest, err := handle.WriteJSON("sample.json", payload{Name: "gallery"})
	if err != nil {
		t.Fatalf("WriteJSON returned %v", err)
	}
	if digest == "" {
		t.Fatal("WriteJSON returned no digest")
	}
	if _, err := os.Stat(handle.Path("sample.json.tmp")); err == nil {
		t.Fatal("the temporary file was left behind")
	}
	var loaded payload
	if err := handle.ReadJSON("sample.json", &loaded); err != nil {
		t.Fatalf("ReadJSON returned %v", err)
	}
	if loaded.Name != "gallery" {
		t.Fatalf("ReadJSON produced %+v", loaded)
	}
	if err := handle.ReadJSON("absent.json", &loaded); err == nil {
		t.Fatal("reading a missing artefact was accepted")
	}
	stored, err := handle.FileDigest("sample.json")
	if err != nil {
		t.Fatalf("FileDigest returned %v", err)
	}
	if stored != digest {
		t.Fatalf("digest changed from %s to %s", digest, stored)
	}
	if missing, err := handle.FileDigest("absent.json"); err != nil || missing != "" {
		t.Fatalf("FileDigest on a missing file returned %q, %v", missing, err)
	}
	second, err := handle.WriteJSON("sample.json", payload{Name: "gallery"})
	if err != nil || second != digest {
		t.Fatalf("rewriting the same value produced %s, %v", second, err)
	}
}

func TestMetadataRoundTrip(t *testing.T) {
	handle := openTemp(t)
	if _, err := handle.LoadMetadata(); err == nil {
		t.Fatal("loading absent metadata was accepted")
	}
	metadata := Metadata{Cave: "Store Cave", RecordCount: 2, LedgerDigest: "abc"}
	if err := handle.WriteMetadata(metadata); err != nil {
		t.Fatalf("WriteMetadata returned %v", err)
	}
	loaded, err := handle.LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata returned %v", err)
	}
	if loaded.Schema != MetadataSchema || loaded.Generator != Generator {
		t.Fatalf("metadata header is %+v", loaded)
	}
	if loaded.Cave != "Store Cave" || loaded.RecordCount != 2 {
		t.Fatalf("metadata body is %+v", loaded)
	}
}

func TestMetadataRejectsForeignSchema(t *testing.T) {
	handle := openTemp(t)
	if err := os.WriteFile(handle.Path(MetadataFile), []byte(`{"schema":99,"generator":"x","ledgerDigest":"","auditHead":"","auditEntries":0,"recordCount":0,"instrumentCount":0,"tripCount":0,"stationCount":0,"shotCount":0}`), 0o600); err != nil {
		t.Fatalf("writing metadata: %v", err)
	}
	if _, err := handle.LoadMetadata(); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestAuditChainGrowsAndVerifies(t *testing.T) {
	handle := openTemp(t)
	empty, err := handle.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit returned %v", err)
	}
	if !empty.Valid || empty.EntryCount != 0 || empty.Head != GenesisHash {
		t.Fatalf("an empty chain verified as %+v", empty)
	}
	first, err := handle.Record("import", LedgerFile, "records=2", DigestString("payload-1"))
	if err != nil {
		t.Fatalf("Record returned %v", err)
	}
	if first.Seq != 1 || first.PrevHash != GenesisHash || first.Hash == "" {
		t.Fatalf("first entry is %+v", first)
	}
	second, err := handle.Record("reduce", SnapshotFile, "stations=3", DigestString("payload-2"))
	if err != nil {
		t.Fatalf("Record returned %v", err)
	}
	if second.Seq != 2 || second.PrevHash != first.Hash {
		t.Fatalf("second entry is %+v", second)
	}
	verification, err := handle.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit returned %v", err)
	}
	if !verification.Valid || verification.EntryCount != 2 || verification.Head != second.Hash {
		t.Fatalf("verification is %+v", verification)
	}
	if _, err := handle.Record("  ", "", "", ""); err == nil {
		t.Fatal("an empty action was accepted")
	}
}

func TestVerifyAuditDetectsTamperedPayload(t *testing.T) {
	handle := openTemp(t)
	if _, err := handle.Record("import", LedgerFile, "records=2", DigestString("payload")); err != nil {
		t.Fatalf("Record returned %v", err)
	}
	raw, err := os.ReadFile(handle.Path(AuditFile))
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}
	tampered := strings.Replace(string(raw), `"records=2"`, `"records=9"`, 1)
	if tampered == string(raw) {
		t.Fatal("the fixture did not contain the expected detail")
	}
	if err := os.WriteFile(handle.Path(AuditFile), []byte(tampered), 0o600); err != nil {
		t.Fatalf("writing audit log: %v", err)
	}
	verification, err := handle.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit returned %v", err)
	}
	if verification.Valid {
		t.Fatal("a tampered entry verified as valid")
	}
	if verification.BrokenAt != 1 || !strings.Contains(verification.Reason, "does not match") {
		t.Fatalf("verification is %+v", verification)
	}
}

func TestVerifyAuditDetectsBrokenLink(t *testing.T) {
	handle := openTemp(t)
	for _, action := range []string{"import", "reduce", "adjust"} {
		if _, err := handle.Record(action, LedgerFile, action, DigestString(action)); err != nil {
			t.Fatalf("Record returned %v", err)
		}
	}
	entries, err := handle.LoadAudit()
	if err != nil {
		t.Fatalf("LoadAudit returned %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("audit log holds %d entries", len(entries))
	}
	raw, err := os.ReadFile(handle.Path(AuditFile))
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	trimmed := strings.Join([]string{lines[0], lines[2]}, "\n") + "\n"
	if err := os.WriteFile(handle.Path(AuditFile), []byte(trimmed), 0o600); err != nil {
		t.Fatalf("writing audit log: %v", err)
	}
	verification, err := handle.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit returned %v", err)
	}
	if verification.Valid {
		t.Fatal("a chain with a removed entry verified as valid")
	}
	if verification.BrokenAt != 2 {
		t.Fatalf("verification is %+v", verification)
	}
}

func TestDigestHelpers(t *testing.T) {
	if Digest(nil) != DigestString("") {
		t.Fatal("Digest and DigestString disagree on the empty input")
	}
	if len(Digest([]byte("a"))) != 64 {
		t.Fatalf("digest length is %d", len(Digest([]byte("a"))))
	}
	if Digest([]byte("a")) == Digest([]byte("b")) {
		t.Fatal("different inputs produced the same digest")
	}
	if short("abc") != "abc" {
		t.Fatal("short truncated a small digest")
	}
	if len(short(Digest([]byte("a")))) != 12 {
		t.Fatal("short did not truncate a full digest")
	}
}

func TestPathHelpers(t *testing.T) {
	handle := openTemp(t)
	if handle.LedgerPath() != handle.Path(LedgerFile) {
		t.Fatal("LedgerPath disagrees with Path")
	}
	if filepath.Dir(handle.Path(AuditFile)) != handle.Root() {
		t.Fatal("artefacts should live directly in the store root")
	}
}
