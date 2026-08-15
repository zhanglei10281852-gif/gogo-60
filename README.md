# CaveLoop

CaveLoop is an offline command line tool for **speleological survey data reduction and
traverse network adjustment**. It takes raw cave survey field notes (tape distance,
compass azimuth, clinometer inclination, optional backsights), reduces them into
three dimensional station coordinates, analyses the passage network, finds the
independent loop basis, measures how well each loop closes, distributes the closure
error and flags the gross errors that typically spoil a survey.

## Independence notice

CaveLoop is an **independent, original project**. It has **no affiliation, endorsement,
sponsorship, or association with any other product, company, or organization**. Any
resemblance of terminology to other survey software is incidental: the vocabulary used
here (foresight, backsight, loop closure, blunder) is the common language of cave
surveying. No third party code, data, trademark, or branding is included.

## Design constraints

- **Go standard library only.** `go.mod` declares no requirements at all.
- **No network access, ever.** Nothing in the code opens a socket, resolves a name or
  fetches a URL. The only I/O is the files you point the CLI at.
- **Deterministic.** Identical input produces byte identical output and byte identical
  store artefacts. See [Determinism](#determinism).
- **Strict input.** Every JSON document is decoded with unknown fields rejected and
  trailing content rejected.

Requires Go 1.22.5 or newer to build.

## Install and build

```sh
go build ./...
go test ./... -count=1
go vet ./...
go build -o caveloop ./cmd/caveloop
```

With `GOTOOLCHAIN=local` and `GOPROXY=off` set, the build stays entirely offline.

## Quick start

```sh
# 1. check a document before it touches the store
caveloop validate -input examples/survey.json

# 2. append it to the append only ledger
caveloop import -input examples/survey.json -data ./store
caveloop import -input examples/followup.jsonl -data ./store

# 3. reduce the ledger into coordinates and write the network snapshot
caveloop reduce -data ./store

# 4. inspect the network
caveloop network -data ./store
caveloop loops   -data ./store
caveloop blunders -data ./store

# 5. distribute the loop closure error and store the adjusted snapshot
caveloop adjust -data ./store

# 6. full report and integrity check
caveloop report -data ./store
caveloop verify -data ./store
```

## Subcommands

| Subcommand | What it does                                                                                              |
| ---------- | --------------------------------------------------------------------------------------------------------- |
| `validate` | Strictly decodes a survey document and reports every structural and physical finding. Writes nothing.     |
| `import`   | Validates a document and appends its records to the ledger, then records the action in the audit chain.   |
| `reduce`   | Folds the ledger into a survey, reduces every leg, lays out coordinates, and writes the network snapshot. |
| `adjust`   | Same as `reduce` plus deterministic loop closure distribution; writes the adjusted snapshot.              |
| `network`  | Reports the graph topology: components, junctions, dangling passages, duplicate legs, name collisions.    |
| `loops`    | Lists the independent loop basis with horizontal, vertical, total and relative closure error.             |
| `blunders` | Runs the heuristic blunder detectors and explains each suspicion.                                         |
| `report`   | Prints the full survey report: metrics, topology, closure, extremes, per trip statistics.                 |
| `verify`   | Replays the audit hash chain and compares the stored digests against the files on disk.                   |

### Common flags

| Flag           | Meaning                                                                                |
| -------------- | -------------------------------------------------------------------------------------- |
| `-config path` | Strict JSON configuration document. Omitted means built in defaults.                   |
| `-data dir`    | Store directory. Overrides `dataDir` from the configuration.                           |
| `-format kind` | `text` (default) or `json`.                                                            |
| `-out path`    | Write the report to a file instead of standard output. Parent directories are created. |

### Subcommand flags

| Flag                 | Subcommands          | Meaning                                                                   |
| -------------------- | -------------------- | ------------------------------------------------------------------------- |
| `-input path`        | `validate`, `import` | Survey document to read. Required.                                        |
| `-input-format kind` | `validate`, `import` | `json`, `jsonl` or `auto` (from the file extension).                      |
| `-strict`            | `validate`           | Treat warnings as a failure.                                              |
| `-no-write`          | `reduce`, `adjust`   | Compute and report without touching the stored snapshot.                  |
| `-adjusted`          | `loops`, `report`    | Report the adjusted network. Default is off for `loops`, on for `report`. |
| `-fail-on-finding`   | `blunders`           | Exit with code 2 when at least one blunder is suspected.                  |

### Exit codes

| Code | Meaning                                                                                                                       |
| ---- | ----------------------------------------------------------------------------------------------------------------------------- |
| `0`  | The command succeeded.                                                                                                        |
| `1`  | The command could not run: bad flag, unreadable file, invalid configuration.                                                  |
| `2`  | The command ran and the data was refused: invalid survey, broken audit chain, findings under `-strict` or `-fail-on-finding`. |

## Survey input format

Two interchangeable encodings are accepted.

### Single JSON document (`.json`)

```json
{
  "cave": "Gouffre du Lievre Blanc",
  "region": "Plateau de Vercorin",
  "instruments": [
    {
      "id": "set-a",
      "lengthUnit": "m",
      "angleUnit": "deg",
      "tapeCorrection": -0.02,
      "tapeScale": 1.0,
      "azimuthCorrection": 0.5,
      "inclinationCorrection": -0.2,
      "declination": 2.5
    }
  ],
  "trips": [
    {
      "id": "T1",
      "name": "Entrance series",
      "date": "2031-04-12",
      "surveyors": ["R. Vasquez"],
      "lengthUnit": "m",
      "angleUnit": "deg",
      "declination": 2.5,
      "instrument": "set-a",
      "stations": [
        {
          "name": "E0",
          "flags": ["entrance", "fixed", "surface"],
          "fixed": { "east": 0.0, "north": 0.0, "up": 0.0, "unit": "m" },
          "note": "shaft head bolt"
        },
        { "name": "A1" }
      ],
      "shots": [
        {
          "id": "S001",
          "from": "E0",
          "to": "A1",
          "distance": 12.5,
          "azimuth": 90.0,
          "inclination": -5.0,
          "backAzimuth": 270.5,
          "backInclination": 5.2,
          "backDistance": 12.52,
          "instrument": "set-a",
          "excluded": false,
          "note": "roof channel"
        }
      ]
    }
  ]
}
```

### JSON Lines (`.jsonl`, `.ndjson`)

One record per line. `kind` is `instrument` or `trip`, and exactly the matching payload
field must be present:

```jsonl
{"kind":"instrument","cave":"Gouffre du Lievre Blanc","instrument":{"id":"set-c","angleUnit":"grad"}}
{"kind":"trip","cave":"Gouffre du Lievre Blanc","trip":{"id":"T4","angleUnit":"grad","shots":[]}}
```

Blank lines are skipped. Every other line must be a complete JSON value. This is also
the on disk format of the ledger, so a ledger can be replayed as input.

### Field semantics

- `lengthUnit`: `m` / `meters` / `metre` / `metres`, or `ft` / `feet` / `foot`.
- `angleUnit`: `deg` / `degrees` / `d`, or `grad` / `grads` / `gon` / `gons`.
- A trip inherits the configured defaults when it omits a unit. An instrument inherits
  the units of the trip that references it.
- `declination` is resolved instrument first, then trip, then configuration default. It
  is expressed in the angle unit of whichever level declared it.
- Corrections are applied as `distance = raw * tapeScale + tapeCorrection`,
  `azimuth = raw + azimuthCorrection + declination`,
  `inclination = raw + inclinationCorrection`.
- Azimuths are wrapped into `[0, 360)` degrees. Inclinations must resolve into
  `[-90, +90]` degrees after correction or the survey is refused.
- Station flags: `fixed`, `entrance`, `surface`. A `fixed` station must carry a `fixed`
  coordinate block, and a station carrying one is treated as fixed.
- `excluded: true` keeps a leg in the ledger and in the reports but removes it from the
  network, the traverse and the metrics.
- Records are folded by identifier: a later trip or instrument with the same `id`
  replaces the earlier one, which gives the append only ledger an upsert semantic.

## Configuration format

Every key is optional; omitted keys keep the built in default shown here. Unknown keys
are rejected. All tolerances are in **meters** and **decimal degrees** regardless of the
units used in the field notes.

```json
{
  "version": 1,
  "dataDir": "./caveloop-data",
  "defaults": { "lengthUnit": "m", "angleUnit": "deg", "declination": 0.0 },
  "tolerances": {
    "backsightAzimuthDeg": 2.0,
    "backsightInclinationDeg": 2.0,
    "backsightDistanceMeters": 0.1,
    "backsightDistanceRatio": 0.02,
    "loopClosureMeters": 0.5,
    "loopClosurePpm": 20000,
    "verticalClosureMeters": 0.3
  },
  "adjustment": {
    "enabled": true,
    "maxPasses": 24,
    "convergenceMeters": 0.0005,
    "minShotWeightMeters": 0.05,
    "adjustVertical": true
  },
  "blunders": {
    "enabled": true,
    "reversedWindowDeg": 12.0,
    "lengthOutlierSigma": 3.0,
    "lengthOutlierMinimumShots": 6,
    "transposeImprovementRatio": 0.6,
    "maxCandidatesPerLoop": 32
  },
  "output": { "format": "text", "lengthPrecision": 3, "anglePrecision": 2 }
}
```

## How the reduction works

1. **Validation** checks identifiers, references, units, dates, flags and the physical
   range of every reading, and returns sorted findings with stable codes.
2. **Reduction** resolves the instrument set of each leg, applies tape scale, tape
   correction, compass correction, clinometer correction and magnetic declination,
   converts everything to meters and decimal degrees, then reconciles the foresight
   against the backsight. A reciprocal reading within tolerance is averaged (circular
   mean for azimuth); outside tolerance the foresight is kept and a warning is raised.
3. **Graph construction** builds an undirected multigraph. Parallel legs between the
   same pair of stations are kept, because that is exactly what closes a loop.
4. **Topology** derives connected components (deterministic union find), junctions
   (three or more distinct passages), dangling passages (degree one, not a control
   point), isolated stations, duplicate legs and station names that differ only by case
   or padding.
5. **Traverse** anchors every component on its lexicographically first control station,
   or on a synthetic origin when it has none, and propagates coordinates along a
   shortest path spanning tree so a station is always reached through the least
   accumulated tape. A control station that is not the anchor produces a residual.
6. **Loop basis** is the fundamental cycle basis: each leg outside the spanning forest
   closes exactly one independent loop with the tree path between its endpoints. The
   loop count equals the cyclomatic number, so the basis is neither redundant nor
   incomplete. Each loop reports horizontal, vertical, total and relative (ppm) error.
7. **Adjustment** distributes each loop residual over the legs of that loop in
   proportion to tape length, repeating passes in loop identifier order until the
   largest residual falls under the convergence threshold. Tape lengths are never
   modified, only the displacement vectors and therefore the coordinates.
8. **Blunder detection** covers a reversed reading (reciprocal disagreement near
   180 degrees), a reversed leg inside a failing loop, transposed azimuth digits
   (every digit swap of every candidate leg is re-closed against the loop), a gross tape
   outlier measured in standard deviations from the trip mean, a loop closing outside
   tolerance, and a misclosure dominated by its vertical component.
9. **Metrics** report surveyed length, horizontal length, vertical range, maximum depth,
   the deepest and highest stations, the longest leg, the longest and deepest trips, the
   bounding box and per trip statistics.

## Store layout

A store is a plain directory:

| File            | Content                                                                                                                 |
| --------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `ledger.jsonl`  | Append only stream of survey records, one JSON value per line.                                                          |
| `network.json`  | The last computed network snapshot (stations, legs, topology, loops, adjustment, metrics, blunders, settings in force). |
| `metadata.json` | Counters plus the ledger digest, snapshot digest and audit head.                                                        |
| `audit.jsonl`   | SHA-256 hash chained audit log.                                                                                         |

Whole file writes go through a temporary file followed by a rename, so a reader never
observes a partially written artefact. The ledger is appended with a single write of all
new records.

Each audit entry links to its predecessor:

```
hash = SHA-256(seq \n action \n target \n detail \n payloadDigest \n prevHash)
```

The first entry links to 64 zeros. `caveloop verify` replays the chain, reports the
first inconsistency by sequence number, and cross checks the recorded digests against
the files on disk.

## Determinism

- Maps are never iterated for output: every collection is sorted before it is emitted
  (stations by name, legs by trip then shot, loops by chord, issues by severity, path,
  code, message).
- Floating point values are rounded to a fixed number of decimals before formatting,
  with half away from zero rounding and no signed zero.
- No timestamps, hostnames, user names, process identifiers or random values are stored
  or printed anywhere.
- The spanning tree, the loop basis and the adjustment passes all use explicit
  tie breaking rules, so no result depends on map order or scheduling.
- JSON is emitted with two space indentation and HTML escaping disabled.

Running the same import and adjust sequence twice into two different directories
produces byte identical `ledger.jsonl`, `network.json`, `metadata.json` and
`audit.jsonl`. This is asserted by a test in `internal/cli`.

## Docker

The image is a multi stage build: a `golang:1.22` builder with `GOTOOLCHAIN=local` and
`CGO_ENABLED=0`, and a `scratch` final stage containing nothing but the static binary.

```sh
docker build -t caveloop:local .

# the container needs no network at all
docker run --rm --network none \
  -v "$PWD/examples:/work/examples" \
  -v "$PWD/store:/work/store" \
  caveloop:local import -input /work/examples/survey.json -data /work/store

docker run --rm --network none \
  -v "$PWD/store:/work/store" \
  caveloop:local adjust -data /work/store

docker run --rm --network none \
  -v "$PWD/store:/work/store" \
  caveloop:local report -data /work/store
```

`ENTRYPOINT` is the binary, so arguments passed to `docker run` are CaveLoop
subcommands and flags. Because the final stage is `scratch` there is no shell inside the
image; use `caveloop:local help` to see the command surface.

## Repository layout

```
cmd/caveloop            process entry point
internal/units          unit parsing, conversion, azimuth and inclination normalisation
internal/geom           east/north/up vectors, polar conversion, descriptive statistics
internal/jsonx          strict JSON and JSON Lines decoding, canonical encoding
internal/model          survey data model, strict decoding, validation
internal/config         configuration schema, overlay merge, validation
internal/reduce         corrections, unit conversion, foresight/backsight reconciliation
internal/network        survey graph and topology analysis
internal/traverse       coordinate layout and control residuals
internal/loops          independent loop basis and closure errors
internal/adjust         deterministic closure error distribution
internal/blunder        heuristic gross error detection
internal/metrics        derived survey statistics
internal/store          ledger, snapshot, metadata, hash chained audit log
internal/pipeline       stage orchestration and snapshot construction
internal/report         text and JSON rendering
internal/cli            subcommands, flags, exit codes
examples/               sample survey documents and a sample configuration
```

## Licence

No licence is granted by this repository. It is published as an original sample project.
