# CaveLoop release report

CaveLoop is an independent, original project with no affiliation, endorsement,
sponsorship, or association with any other product, company, or organization.

- Module: `CaveLoop`
- Go directive: `go 1.22.5`
- Dependencies: **none** (Go standard library only, `go.mod` has no `require` block)
- Network access: none anywhere in the code, tests or Docker build
- Generated code: none

## Effective production LOC

Counting rule: all non `_test.go` Go files, excluding blank lines and comment only
lines (both `//` and `/* ... */` bodies).

| File                            | Effective LOC |
| ------------------------------- | ------------- |
| `cmd/caveloop/main.go`          | 8             |
| `internal/adjust/adjust.go`     | 222           |
| `internal/blunder/blunder.go`   | 340           |
| `internal/cli/cli.go`           | 193           |
| `internal/cli/commands.go`      | 452           |
| `internal/config/config.go`     | 341           |
| `internal/geom/geom.go`         | 144           |
| `internal/jsonx/jsonx.go`       | 125           |
| `internal/loops/loops.go`       | 280           |
| `internal/metrics/metrics.go`   | 244           |
| `internal/model/decode.go`      | 132           |
| `internal/model/model.go`       | 278           |
| `internal/model/validate.go`    | 372           |
| `internal/network/network.go`   | 400           |
| `internal/pipeline/pipeline.go` | 333           |
| `internal/reduce/reduce.go`     | 552           |
| `internal/report/report.go`     | 297           |
| `internal/report/text.go`       | 649           |
| `internal/store/store.go`       | 348           |
| `internal/traverse/traverse.go` | 271           |
| `internal/units/units.go`       | 221           |
| **Total**                       | **6202**      |

Requirement was at least 2600 effective production LOC. Delivered: **6202**.

Test code, which is excluded from the figure above, adds **3821** effective LOC across
**17** `_test.go` files.

## Packages

| Import path                  | Responsibility                                                                                                                                                       |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CaveLoop/cmd/caveloop`      | `main`, forwards process arguments to the CLI and maps the returned code to the exit status                                                                          |
| `CaveLoop/internal/units`    | length and angle unit parsing and conversion, azimuth wrapping, inclination validation, deterministic rounding and formatting, ppm                                   |
| `CaveLoop/internal/geom`     | east/north/up vector algebra, polar to cartesian conversion, bounding box, mean / median / standard deviation                                                        |
| `CaveLoop/internal/jsonx`    | strict JSON decoding (unknown fields and trailing content rejected), JSON Lines reader, canonical encoders                                                           |
| `CaveLoop/internal/model`    | survey data model (instruments, trips, stations, shots, ledger records), strict document and stream decoding, canonical ordering, validation with stable issue codes |
| `CaveLoop/internal/config`   | configuration schema, pointer overlay merge that preserves defaults, cross field validation                                                                          |
| `CaveLoop/internal/reduce`   | instrument resolution, tape / compass / clinometer corrections, magnetic declination, unit conversion, foresight and backsight reconciliation, station merging       |
| `CaveLoop/internal/network`  | undirected survey multigraph, connected components, junctions, dangling passages, isolated stations, duplicate legs, station name collisions                         |
| `CaveLoop/internal/traverse` | shortest path spanning forest layout of station coordinates, per component datum and depth, control residuals                                                        |
| `CaveLoop/internal/loops`    | fundamental cycle basis, circuit reconstruction, horizontal / vertical / total / relative closure error and tolerance classification                                 |
| `CaveLoop/internal/adjust`   | iterative proportional closure error distribution, per leg corrections, loop residuals before and after                                                              |
| `CaveLoop/internal/blunder`  | reversed reading, reversed leg, transposed azimuth digits, gross length outlier, loop closure exceeded, vertical dominant misclosure                                 |
| `CaveLoop/internal/metrics`  | surveyed length, horizontal length, vertical range, maximum depth, extremes, bounding box, per trip statistics                                                       |
| `CaveLoop/internal/store`    | append only JSONL ledger, network snapshot, metadata, atomic whole file writes, SHA-256 hash chained audit log and verification                                      |
| `CaveLoop/internal/pipeline` | stage orchestration, snapshot construction, persistence plus audit recording                                                                                         |
| `CaveLoop/internal/report`   | text tables and indented JSON payloads for every subcommand                                                                                                          |
| `CaveLoop/internal/cli`      | subcommand dispatch, shared flags, session and output resolution, exit code policy                                                                                   |

17 packages in total (16 library packages plus `main`).

## Validation results

All commands were run with `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOCACHE` and `GOTMPDIR`
redirected under the ignored `.cache/` directory.

| Check              | Command                                                                                                            | Result                                                                        |
| ------------------ | ------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| Formatting         | `gofmt -l .`                                                                                                       | clean, no files listed                                                        |
| Build              | `go build ./...`                                                                                                   | exit 0, no output                                                             |
| Vet                | `go vet ./...`                                                                                                     | exit 0, no output                                                             |
| Tests              | `go test ./... -count=1`                                                                                           | exit 0, all 16 library packages `ok`                                          |
| Docker build       | `docker build -t caveloop:local .`                                                                                 | exit 0, image built                                                           |
| Docker run         | `docker run --rm --network none ... caveloop:local <subcommand>`                                                   | import, adjust and verify all exit 0                                          |
| CLI smoke workflow | `validate`, `import` x2, `reduce`, `network`, `loops`, `blunders`, `adjust`, `loops -adjusted`, `report`, `verify` | every subcommand exit 0                                                       |
| Determinism        | two independent stores built from the same inputs                                                                  | `ledger.jsonl`, `network.json`, `metadata.json`, `audit.jsonl` byte identical |

### Test package results

```
?       CaveLoop/cmd/caveloop   [no test files]
ok      CaveLoop/internal/adjust
ok      CaveLoop/internal/blunder
ok      CaveLoop/internal/cli
ok      CaveLoop/internal/config
ok      CaveLoop/internal/geom
ok      CaveLoop/internal/jsonx
ok      CaveLoop/internal/loops
ok      CaveLoop/internal/metrics
ok      CaveLoop/internal/model
ok      CaveLoop/internal/network
ok      CaveLoop/internal/pipeline
ok      CaveLoop/internal/reduce
ok      CaveLoop/internal/report
ok      CaveLoop/internal/store
ok      CaveLoop/internal/traverse
ok      CaveLoop/internal/units
```

### Smoke workflow figures

Running the bundled `examples/survey.json` and `examples/followup.jsonl` through the
pipeline produces, reproducibly:

- 21 stations, 21 active legs, 243.371 m of surveyed passage
- 1 connected component anchored on the control station `E0`
- 2 junctions (`A2`, `J1`) and 2 dangling passages (`C2`, `F12`)
- 1 independent loop, `L001`, closing to 0.120 m over 96.810 m (1234.5 ppm), within tolerance
- adjustment converged in 2 passes, distributing 0.120 m over 6 legs, residual 0.000 m
- 2 suspected blunders: a reversed reading on `T4/S102` and a gross length outlier on
  `T4/S111` at 3.30 standard deviations
- vertical range 18.307 m, deepest station `F12`, longest leg `T4/S111`
- ledger digest `38cd8049e62f05e76dbf92d364d756fb19d1499f60a832b7aed07c77a40363d3`
- snapshot digest `fb615e42efdd1518867c122abc7d4459e00c83c6e8d4201df21b7fa3f2efdf7a`

The same two digests are produced by the `scratch` based Docker image running with
`--network none`, confirming the output does not depend on the host.

## Docker image

- Builder stage: `FROM golang:1.22` with `GOTOOLCHAIN=local`, `CGO_ENABLED=0`,
  `GOOS=linux`, `GOPROXY=off`, building `./cmd/caveloop` with `-trimpath`
- Final stage: `FROM scratch`, containing only `/caveloop`
- `ENTRYPOINT ["/caveloop"]`, so `docker run` arguments are CaveLoop subcommands
- `.dockerignore` excludes `.cache/`, `.git/`, tests, examples and documentation from the
  build context

## Repository hygiene

- `.gitignore` ignores `.cache/`, `caveloop-data/`, binaries and editor noise
- All build cache, temporary files and smoke artefacts were written under `.cache/`
- Git history: branch `main`, exactly one root commit, author
  `GoMark Author <gomark@example.invalid>`, no remotes, clean working tree
