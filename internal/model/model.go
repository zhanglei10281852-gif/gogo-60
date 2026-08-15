// Package model defines the CaveLoop survey data model: instrument sets, trips,
// stations and shots, together with the canonical ordering rules that make
// every downstream computation reproducible.
package model

import (
	"fmt"
	"sort"
	"strings"
)

// Station flag tokens recognised by CaveLoop.
const (
	FlagFixed    = "fixed"
	FlagEntrance = "entrance"
	FlagSurface  = "surface"
)

// RecordKind values accepted by the JSON Lines survey format.
const (
	KindInstrument = "instrument"
	KindTrip       = "trip"
)

// knownFlags lists every accepted station flag in canonical order.
var knownFlags = []string{FlagFixed, FlagEntrance, FlagSurface}

// Instrument describes a set of survey instruments and the systematic
// corrections that must be applied to every measurement taken with it.
//
// Correction values are expressed in the instrument's own declared units. When
// those units are omitted the instrument inherits the units of the trip that
// references it.
type Instrument struct {
	ID                    string   `json:"id"`
	Description           string   `json:"description,omitempty"`
	LengthUnit            string   `json:"lengthUnit,omitempty"`
	AngleUnit             string   `json:"angleUnit,omitempty"`
	TapeCorrection        float64  `json:"tapeCorrection,omitempty"`
	TapeScale             *float64 `json:"tapeScale,omitempty"`
	AzimuthCorrection     float64  `json:"azimuthCorrection,omitempty"`
	InclinationCorrection float64  `json:"inclinationCorrection,omitempty"`
	Declination           *float64 `json:"declination,omitempty"`
}

// FixedCoordinate is a control coordinate for a station.
type FixedCoordinate struct {
	East  float64 `json:"east"`
	North float64 `json:"north"`
	Up    float64 `json:"up"`
	Unit  string  `json:"unit,omitempty"`
}

// Station is a named survey point. Only stations carrying control coordinates
// need a Fixed block; every other station is positioned by traverse.
type Station struct {
	Name  string           `json:"name"`
	Flags []string         `json:"flags,omitempty"`
	Fixed *FixedCoordinate `json:"fixed,omitempty"`
	Note  string           `json:"note,omitempty"`
}

// HasFlag reports whether the station carries the given flag.
func (s Station) HasFlag(flag string) bool {
	for _, candidate := range s.Flags {
		if strings.EqualFold(strings.TrimSpace(candidate), flag) {
			return true
		}
	}
	return false
}

// CanonicalFlags returns the station flags in canonical order, lowercased and
// de-duplicated, so that snapshots do not depend on input ordering.
func (s Station) CanonicalFlags() []string {
	present := make(map[string]bool, len(s.Flags))
	for _, flag := range s.Flags {
		present[strings.ToLower(strings.TrimSpace(flag))] = true
	}
	ordered := make([]string, 0, len(present))
	for _, flag := range knownFlags {
		if present[flag] {
			ordered = append(ordered, flag)
		}
	}
	extra := make([]string, 0, len(present))
	for flag := range present {
		if flag == "" || isKnownFlag(flag) {
			continue
		}
		extra = append(extra, flag)
	}
	sort.Strings(extra)
	return append(ordered, extra...)
}

// isKnownFlag reports whether flag is part of the canonical flag set.
func isKnownFlag(flag string) bool {
	for _, known := range knownFlags {
		if known == flag {
			return true
		}
	}
	return false
}

// KnownFlags exposes a copy of the canonical flag list.
func KnownFlags() []string {
	out := make([]string, len(knownFlags))
	copy(out, knownFlags)
	return out
}

// Shot is one measured leg between two stations. Backsight fields are optional;
// when present they are reconciled against the foresight during reduction.
type Shot struct {
	ID              string   `json:"id"`
	From            string   `json:"from"`
	To              string   `json:"to"`
	Distance        float64  `json:"distance"`
	Azimuth         float64  `json:"azimuth"`
	Inclination     float64  `json:"inclination"`
	BackDistance    *float64 `json:"backDistance,omitempty"`
	BackAzimuth     *float64 `json:"backAzimuth,omitempty"`
	BackInclination *float64 `json:"backInclination,omitempty"`
	Instrument      string   `json:"instrument,omitempty"`
	Excluded        bool     `json:"excluded,omitempty"`
	Note            string   `json:"note,omitempty"`
}

// HasBacksight reports whether any reciprocal reading is available.
func (s Shot) HasBacksight() bool {
	return s.BackAzimuth != nil || s.BackInclination != nil || s.BackDistance != nil
}

// Trip is a single survey session. It carries the units used in the field, the
// magnetic declination applied to the compass readings, the stations that were
// established and the shots that were measured.
type Trip struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Date        string    `json:"date,omitempty"`
	Surveyors   []string  `json:"surveyors,omitempty"`
	LengthUnit  string    `json:"lengthUnit,omitempty"`
	AngleUnit   string    `json:"angleUnit,omitempty"`
	Declination *float64  `json:"declination,omitempty"`
	Instrument  string    `json:"instrument,omitempty"`
	Stations    []Station `json:"stations,omitempty"`
	Shots       []Shot    `json:"shots,omitempty"`
}

// StationByName returns the station declaration with the given name.
func (t Trip) StationByName(name string) (Station, bool) {
	for _, station := range t.Stations {
		if station.Name == name {
			return station, true
		}
	}
	return Station{}, false
}

// Survey is a complete cave data set: the instrument catalogue plus every trip.
type Survey struct {
	Cave        string       `json:"cave"`
	Region      string       `json:"region,omitempty"`
	Instruments []Instrument `json:"instruments,omitempty"`
	Trips       []Trip       `json:"trips,omitempty"`
}

// Record is one line of the JSON Lines survey format and one entry of the
// append only ledger. Exactly one payload field must be populated.
type Record struct {
	Kind       string      `json:"kind"`
	Cave       string      `json:"cave,omitempty"`
	Region     string      `json:"region,omitempty"`
	Instrument *Instrument `json:"instrument,omitempty"`
	Trip       *Trip       `json:"trip,omitempty"`
}

// InstrumentIndex maps instrument identifiers to their declaration.
type InstrumentIndex map[string]Instrument

// Index builds a lookup table of the survey instruments.
func (s Survey) Index() InstrumentIndex {
	index := make(InstrumentIndex, len(s.Instruments))
	for _, instrument := range s.Instruments {
		index[instrument.ID] = instrument
	}
	return index
}

// SortedInstrumentIDs lists instrument identifiers in lexicographic order.
func (s Survey) SortedInstrumentIDs() []string {
	ids := make([]string, 0, len(s.Instruments))
	for _, instrument := range s.Instruments {
		ids = append(ids, instrument.ID)
	}
	sort.Strings(ids)
	return ids
}

// StationNames lists every station referenced anywhere in the survey, sorted.
func (s Survey) StationNames() []string {
	seen := make(map[string]bool)
	for _, trip := range s.Trips {
		for _, station := range trip.Stations {
			seen[station.Name] = true
		}
		for _, shot := range trip.Shots {
			seen[shot.From] = true
			seen[shot.To] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ShotCount totals the shots across every trip.
func (s Survey) ShotCount() int {
	total := 0
	for _, trip := range s.Trips {
		total += len(trip.Shots)
	}
	return total
}

// Canonical returns a copy of the survey with trips sorted by identifier,
// shots sorted by identifier within each trip, stations sorted by name and
// station flags canonicalised. Reduction always runs on a canonical survey.
func (s Survey) Canonical() Survey {
	out := Survey{Cave: s.Cave, Region: s.Region}
	out.Instruments = make([]Instrument, len(s.Instruments))
	copy(out.Instruments, s.Instruments)
	sort.Slice(out.Instruments, func(i, j int) bool {
		return out.Instruments[i].ID < out.Instruments[j].ID
	})
	out.Trips = make([]Trip, len(s.Trips))
	for i, trip := range s.Trips {
		copied := trip
		copied.Surveyors = make([]string, len(trip.Surveyors))
		copy(copied.Surveyors, trip.Surveyors)
		sort.Strings(copied.Surveyors)
		copied.Stations = make([]Station, len(trip.Stations))
		for j, station := range trip.Stations {
			station.Flags = station.CanonicalFlags()
			copied.Stations[j] = station
		}
		sort.Slice(copied.Stations, func(a, b int) bool {
			return copied.Stations[a].Name < copied.Stations[b].Name
		})
		copied.Shots = make([]Shot, len(trip.Shots))
		copy(copied.Shots, trip.Shots)
		sort.Slice(copied.Shots, func(a, b int) bool {
			return copied.Shots[a].ID < copied.Shots[b].ID
		})
		out.Trips[i] = copied
	}
	sort.Slice(out.Trips, func(i, j int) bool { return out.Trips[i].ID < out.Trips[j].ID })
	return out
}

// Records flattens the survey into the ledger record stream. Instruments are
// emitted before trips so that a replay always resolves references.
func (s Survey) Records() []Record {
	canonical := s.Canonical()
	records := make([]Record, 0, len(canonical.Instruments)+len(canonical.Trips))
	for i := range canonical.Instruments {
		instrument := canonical.Instruments[i]
		records = append(records, Record{
			Kind:       KindInstrument,
			Cave:       canonical.Cave,
			Region:     canonical.Region,
			Instrument: &instrument,
		})
	}
	for i := range canonical.Trips {
		trip := canonical.Trips[i]
		records = append(records, Record{
			Kind:   KindTrip,
			Cave:   canonical.Cave,
			Region: canonical.Region,
			Trip:   &trip,
		})
	}
	return records
}

// SurveyFromRecords rebuilds a survey from a ledger record stream. Later
// records with a duplicate identifier replace earlier ones, which gives the
// append only ledger an upsert semantic while remaining deterministic.
func SurveyFromRecords(records []Record) (Survey, error) {
	survey := Survey{}
	instrumentAt := make(map[string]int)
	tripAt := make(map[string]int)
	for i, record := range records {
		if record.Cave != "" && survey.Cave == "" {
			survey.Cave = record.Cave
		}
		if record.Region != "" && survey.Region == "" {
			survey.Region = record.Region
		}
		switch record.Kind {
		case KindInstrument:
			if record.Instrument == nil {
				return Survey{}, fmt.Errorf("record %d: kind %q requires an instrument payload", i+1, record.Kind)
			}
			if position, ok := instrumentAt[record.Instrument.ID]; ok {
				survey.Instruments[position] = *record.Instrument
				continue
			}
			instrumentAt[record.Instrument.ID] = len(survey.Instruments)
			survey.Instruments = append(survey.Instruments, *record.Instrument)
		case KindTrip:
			if record.Trip == nil {
				return Survey{}, fmt.Errorf("record %d: kind %q requires a trip payload", i+1, record.Kind)
			}
			if position, ok := tripAt[record.Trip.ID]; ok {
				survey.Trips[position] = *record.Trip
				continue
			}
			tripAt[record.Trip.ID] = len(survey.Trips)
			survey.Trips = append(survey.Trips, *record.Trip)
		default:
			return Survey{}, fmt.Errorf("record %d: unsupported kind %q (want %q or %q)", i+1, record.Kind, KindInstrument, KindTrip)
		}
	}
	return survey.Canonical(), nil
}

// Merge folds other into the receiver, replacing entries that share an
// identifier. The result is canonical.
func (s Survey) Merge(other Survey) (Survey, error) {
	combined := append(s.Records(), other.Records()...)
	merged, err := SurveyFromRecords(combined)
	if err != nil {
		return Survey{}, err
	}
	if other.Cave != "" {
		merged.Cave = other.Cave
	}
	if s.Cave != "" {
		merged.Cave = s.Cave
	}
	return merged, nil
}
