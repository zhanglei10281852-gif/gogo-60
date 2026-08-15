// Package store implements the local, offline persistence layer of CaveLoop.
//
// The store is a plain directory holding four artefacts:
//
//	ledger.jsonl    append only stream of survey records
//	network.json    the last computed network snapshot
//	metadata.json   counters and digests describing the store
//	audit.jsonl     a SHA-256 hash chained audit log
//
// Every whole file write goes through a temporary file followed by a rename, so
// a reader never observes a half written artefact. The ledger is appended with a
// single write of all new records. No timestamps are recorded anywhere, which is
// what allows two runs over the same input to produce byte identical stores.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"CaveLoop/internal/jsonx"
	"CaveLoop/internal/model"
)

// Artefact file names inside the store directory.
const (
	LedgerFile   = "ledger.jsonl"
	SnapshotFile = "network.json"
	MetadataFile = "metadata.json"
	AuditFile    = "audit.jsonl"
)

// MetadataSchema is the schema version of metadata.json.
const MetadataSchema = 1

// GenesisHash is the previous hash of the first audit entry.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Generator identifies the writer of a store.
const Generator = "CaveLoop"

// Store is a handle on a store directory.
type Store struct {
	root string
}

// Metadata describes the contents of a store.
type Metadata struct {
	Schema          int    `json:"schema"`
	Generator       string `json:"generator"`
	Cave            string `json:"cave,omitempty"`
	Region          string `json:"region,omitempty"`
	RecordCount     int    `json:"recordCount"`
	InstrumentCount int    `json:"instrumentCount"`
	TripCount       int    `json:"tripCount"`
	StationCount    int    `json:"stationCount"`
	ShotCount       int    `json:"shotCount"`
	LedgerDigest    string `json:"ledgerDigest"`
	SnapshotDigest  string `json:"snapshotDigest,omitempty"`
	AuditHead       string `json:"auditHead"`
	AuditEntries    int    `json:"auditEntries"`
	LastAction      string `json:"lastAction,omitempty"`
}

// AuditEntry is one link of the hash chained audit log.
type AuditEntry struct {
	Seq           int    `json:"seq"`
	Action        string `json:"action"`
	Target        string `json:"target"`
	Detail        string `json:"detail,omitempty"`
	PayloadDigest string `json:"payloadDigest"`
	PrevHash      string `json:"prevHash"`
	Hash          string `json:"hash"`
}

// AuditVerification is the outcome of replaying the audit chain.
type AuditVerification struct {
	EntryCount int    `json:"entryCount"`
	Head       string `json:"head"`
	Valid      bool   `json:"valid"`
	BrokenAt   int    `json:"brokenAt,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Open returns a handle on the store rooted at dir, creating the directory when
// it does not exist yet.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("store directory must not be empty")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving store directory %q: %w", dir, err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("creating store directory %s: %w", absolute, err)
	}
	return &Store{root: absolute}, nil
}

// Root is the absolute path of the store directory.
func (s *Store) Root() string { return s.root }

// Path returns the absolute path of one artefact.
func (s *Store) Path(name string) string { return filepath.Join(s.root, name) }

// LedgerPath is the absolute path of the survey ledger.
func (s *Store) LedgerPath() string { return s.Path(LedgerFile) }

// AppendReport describes the outcome of appending records to the ledger.
type AppendReport struct {
	Appended      int    `json:"appended"`
	TotalRecords  int    `json:"totalRecords"`
	PayloadDigest string `json:"payloadDigest"`
	LedgerDigest  string `json:"ledgerDigest"`
}

// AppendRecords appends survey records to the ledger in one write.
func (s *Store) AppendRecords(records []model.Record) (AppendReport, error) {
	report := AppendReport{}
	if len(records) == 0 {
		return report, errors.New("no records to append")
	}
	payload := make([]byte, 0, len(records)*128)
	for index, record := range records {
		line, err := jsonx.MarshalLine(record)
		if err != nil {
			return AppendReport{}, fmt.Errorf("encoding record %d: %w", index+1, err)
		}
		payload = append(payload, line...)
	}
	handle, err := os.OpenFile(s.LedgerPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return AppendReport{}, fmt.Errorf("opening ledger: %w", err)
	}
	if _, err := handle.Write(payload); err != nil {
		_ = handle.Close()
		return AppendReport{}, fmt.Errorf("appending to ledger: %w", err)
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return AppendReport{}, fmt.Errorf("flushing ledger: %w", err)
	}
	if err := handle.Close(); err != nil {
		return AppendReport{}, fmt.Errorf("closing ledger: %w", err)
	}
	existing, err := s.LoadRecords()
	if err != nil {
		return AppendReport{}, err
	}
	digest, err := s.FileDigest(LedgerFile)
	if err != nil {
		return AppendReport{}, err
	}
	report.Appended = len(records)
	report.TotalRecords = len(existing)
	report.PayloadDigest = Digest(payload)
	report.LedgerDigest = digest
	return report, nil
}

// LoadRecords reads every ledger record in insertion order.
func (s *Store) LoadRecords() ([]model.Record, error) {
	handle, err := os.Open(s.LedgerPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening ledger: %w", err)
	}
	defer func() { _ = handle.Close() }()
	records, err := model.DecodeRecords(handle)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", LedgerFile, err)
	}
	return records, nil
}

// LoadSurvey folds the ledger into a canonical survey.
func (s *Store) LoadSurvey() (model.Survey, error) {
	records, err := s.LoadRecords()
	if err != nil {
		return model.Survey{}, err
	}
	if len(records) == 0 {
		return model.Survey{}, fmt.Errorf("ledger %s is empty, run import first", s.LedgerPath())
	}
	return model.SurveyFromRecords(records)
}

// HasLedger reports whether the ledger already exists.
func (s *Store) HasLedger() bool {
	info, err := os.Stat(s.LedgerPath())
	return err == nil && !info.IsDir()
}

// WriteJSON writes an indented JSON artefact atomically and returns its digest.
func (s *Store) WriteJSON(name string, value any) (string, error) {
	payload, err := jsonx.MarshalIndent(value)
	if err != nil {
		return "", fmt.Errorf("encoding %s: %w", name, err)
	}
	if err := s.writeAtomic(name, payload); err != nil {
		return "", err
	}
	return Digest(payload), nil
}

// ReadJSON strictly decodes a JSON artefact of the store.
func (s *Store) ReadJSON(name string, target any) error {
	handle, err := os.Open(s.Path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s is missing from the store", name)
		}
		return fmt.Errorf("opening %s: %w", name, err)
	}
	defer func() { _ = handle.Close() }()
	if err := jsonx.DecodeStrict(handle, target); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// WriteMetadata stores the metadata artefact.
func (s *Store) WriteMetadata(metadata Metadata) error {
	metadata.Schema = MetadataSchema
	metadata.Generator = Generator
	_, err := s.WriteJSON(MetadataFile, metadata)
	return err
}

// LoadMetadata reads the metadata artefact.
func (s *Store) LoadMetadata() (Metadata, error) {
	var metadata Metadata
	if err := s.ReadJSON(MetadataFile, &metadata); err != nil {
		return Metadata{}, err
	}
	if metadata.Schema != MetadataSchema {
		return Metadata{}, fmt.Errorf("%s declares schema %d, this build understands %d", MetadataFile, metadata.Schema, MetadataSchema)
	}
	return metadata, nil
}

// writeAtomic writes data to name through a temporary file and a rename.
func (s *Store) writeAtomic(name string, data []byte) error {
	target := s.Path(name)
	temporary := target + ".tmp"
	handle, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("creating temporary file for %s: %w", name, err)
	}
	if _, err := handle.Write(data); err != nil {
		_ = handle.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("flushing %s: %w", name, err)
	}
	if err := handle.Close(); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("closing %s: %w", name, err)
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replacing %s: %w", name, err)
	}
	return nil
}

// FileDigest returns the SHA-256 digest of a store artefact.
func (s *Store) FileDigest(name string) (string, error) {
	data, err := os.ReadFile(s.Path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	return Digest(data), nil
}

// Digest returns the lowercase hexadecimal SHA-256 digest of data.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DigestString returns the SHA-256 digest of a string.
func DigestString(text string) string { return Digest([]byte(text)) }

// chainInput builds the canonical pre-image of an audit entry hash.
func chainInput(entry AuditEntry) string {
	return strings.Join([]string{
		fmt.Sprintf("%d", entry.Seq),
		entry.Action,
		entry.Target,
		entry.Detail,
		entry.PayloadDigest,
		entry.PrevHash,
	}, "\n")
}

// Record appends an audit entry describing an action and returns the new head.
func (s *Store) Record(action, target, detail, payloadDigest string) (AuditEntry, error) {
	if strings.TrimSpace(action) == "" {
		return AuditEntry{}, errors.New("audit action must not be empty")
	}
	entries, err := s.LoadAudit()
	if err != nil {
		return AuditEntry{}, err
	}
	previous := GenesisHash
	if len(entries) > 0 {
		previous = entries[len(entries)-1].Hash
	}
	entry := AuditEntry{
		Seq:           len(entries) + 1,
		Action:        action,
		Target:        target,
		Detail:        detail,
		PayloadDigest: payloadDigest,
		PrevHash:      previous,
	}
	entry.Hash = DigestString(chainInput(entry))
	line, err := jsonx.MarshalLine(entry)
	if err != nil {
		return AuditEntry{}, fmt.Errorf("encoding audit entry: %w", err)
	}
	handle, err := os.OpenFile(s.Path(AuditFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return AuditEntry{}, fmt.Errorf("opening audit log: %w", err)
	}
	if _, err := handle.Write(line); err != nil {
		_ = handle.Close()
		return AuditEntry{}, fmt.Errorf("appending audit entry: %w", err)
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return AuditEntry{}, fmt.Errorf("flushing audit log: %w", err)
	}
	if err := handle.Close(); err != nil {
		return AuditEntry{}, fmt.Errorf("closing audit log: %w", err)
	}
	return entry, nil
}

// LoadAudit reads the audit log in order.
func (s *Store) LoadAudit() ([]AuditEntry, error) {
	handle, err := os.Open(s.Path(AuditFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening audit log: %w", err)
	}
	defer func() { _ = handle.Close() }()
	lines, err := jsonx.ReadLines(handle)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", AuditFile, err)
	}
	entries := make([]AuditEntry, 0, len(lines))
	for _, line := range lines {
		var entry AuditEntry
		if err := jsonx.DecodeLine(line, &entry); err != nil {
			return nil, fmt.Errorf("%s: %w", AuditFile, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// VerifyAudit replays the hash chain and reports the first inconsistency.
func (s *Store) VerifyAudit() (AuditVerification, error) {
	entries, err := s.LoadAudit()
	if err != nil {
		return AuditVerification{}, err
	}
	verification := AuditVerification{EntryCount: len(entries), Valid: true}
	previous := GenesisHash
	for index, entry := range entries {
		expectedSeq := index + 1
		if entry.Seq != expectedSeq {
			verification.Valid = false
			verification.BrokenAt = expectedSeq
			verification.Reason = fmt.Sprintf("entry %d declares sequence %d", expectedSeq, entry.Seq)
			return verification, nil
		}
		if entry.PrevHash != previous {
			verification.Valid = false
			verification.BrokenAt = entry.Seq
			verification.Reason = fmt.Sprintf("entry %d links to %s but the previous head is %s", entry.Seq, short(entry.PrevHash), short(previous))
			return verification, nil
		}
		expectedHash := DigestString(chainInput(entry))
		if entry.Hash != expectedHash {
			verification.Valid = false
			verification.BrokenAt = entry.Seq
			verification.Reason = fmt.Sprintf("entry %d hash %s does not match its content, expected %s", entry.Seq, short(entry.Hash), short(expectedHash))
			return verification, nil
		}
		previous = entry.Hash
	}
	verification.Head = previous
	if len(entries) == 0 {
		verification.Head = GenesisHash
	}
	return verification, nil
}

// short truncates a digest for human readable messages.
func short(digest string) string {
	if len(digest) <= 12 {
		return digest
	}
	return digest[:12]
}
