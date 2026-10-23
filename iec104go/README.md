# iec104go

Clean-room, Apache-2.0 IEC 60870-5-104 codec, with no dependencies outside
Go's standard library. Module: `github.com/tonylturner/iec104go`.

**Phase 1a:** APCI, exact-boundary stream reading, ASDU envelopes, 46 process,
system and parameter types, CP56Time2a and CP16Time2a. TCP sessions, timers,
client/server roles and application procedures are **phase 1b**, not implemented
here. This is not a claim of independently certified IEC conformance.

## API

```go
p := iec104go.DefaultParams() // COT=2, CA=2, IOA=3
builder, err := iec104go.NewBuilder(p, iec104go.Header{
    Type: iec104go.M_DP_TB_1, Cause: iec104go.Spontaneous,
    Originator: 0, CommonAddress: 1, // originator is explicit; never injected
})
if err != nil { return err }
err = builder.Append(iec104go.Object{Address: 1001, Element: iec104go.Timed{
    Value: iec104go.DoublePoint{DIQ: 2},
    Time: iec104go.CP56FromTime(time.Now(), false),
}})
if err != nil { return err }
a := builder.ASDU()
payload, err := a.MarshalBinary(p)
if err != nil { return err }
wire, err := (iec104go.Frame{
    Kind: iec104go.IFrame, Send: 0, Receive: 0, ASDU: payload,
}).MarshalBinary()
if err != nil { return err }
_ = wire // the later session layer owns sequence assignment and transmission
```

- `Frame.MarshalBinary` / `DecodeFrame` operate on one complete APDU.
  `NewAPDUReader(io.Reader).ReadFrame()` handles fragmented and coalesced
  streams without read-ahead. No scanning past malformed data. Caller owns
  deadlines, cancellation and synchronization; this codec starts no goroutines.
  I-frame payload structure is checked separately by `DecodeASDU`.
- `DecodeASDU(bytes, params)` returns `Message`, concretely `*ASDU` or
  `*RawASDU`. Both expose `GetHeader()` and `MarshalBinary(params)`.
  `errors.Is(err, ErrMalformed)` identifies structural errors. Unsupported
  types yield a successful `RawASDU` whose `DecodeError` wraps
  `ErrUnsupportedType`; its complete header and opaque payload are retained.
  The application, not the codec, decides rejection precedence and COT 44–47.
- `ASDU{Header, Objects}` supports both SQ modes. `Object{Address, Element}`
  exposes every address even with SQ=1. Typed ASDUs derive count from Objects;
  raw ASDUs use Header.Count. SQ requires contiguous, nonoverflowing addresses.
- Elements are **values**, not pointers: `SinglePoint`, `DoublePoint`,
  `StepPosition`, `Float`, command/setpoint/parameter/system structs and
  `Timed{Value, Time}`. Nil, pointer and nested Timed elements return an error.
  A protection event must be wrapped in Timed (there is no CP24 support).
  Raw descriptor octets preserve reserved bits; helper methods expose state,
  quality and qualifiers. Signed normalized values use the exact Q15 int16
  representation. Float32 values preserve IEEE-754 bits, including NaNs.
- `Builder.Append` packs to 249 ASDU bytes or 127 objects. `ErrFull` leaves
  it unchanged: send the snapshot from `ASDU()`, create a fresh builder and
  retry the object. Other append errors also leave it unchanged. Snapshots
  own their object slices; decoded messages own all bytes. Callers must not
  mutate shared messages while another goroutine encodes them.
- `CP56Time2a` keeps raw fields and flags even for invalid calendars.
  `Time()` interprets the year as 2000–2099 UTC and returns false for invalid
  calendars, independently of the IV bit. `CP56FromTime` emits UTC, DOW=0,
  SU=0, year modulo 100; precision is milliseconds. `RES1` preserves minute
  bit 6 (also called GEN in newer profiles), without interpreting it.
- `Params` must be explicit; its zero value is invalid. Optional smaller
  widths are a codec convenience, not the selected IEC 104 wire profile.

See [PICS.md](PICS.md) for type/COT coverage and profile decisions.

## Validation

From this directory (Go downloads the pinned toolchain if necessary):

```sh
gofmt -l .                       # must print nothing
go vet ./...
go test -race -count=1 -timeout 300s ./...
go test -run='^$' -fuzz=FuzzAPDUReader -fuzztime=20s -parallel=2 -timeout 300s
go test -run='^$' -fuzz=FuzzDecodeASDU -fuzztime=20s -parallel=2 -timeout 300s
go build ./...
```

Golden tests use committed JSON, so neither tshark nor a live peer is required.
[Extraction instructions and capture provenance](testdata/README.md) identify
our Apache-licensed historical packets. No Docker operation is needed.

## Clean-room provenance

No IEC 104 implementation source or our lib60870-shaped C application source
was consulted. Only public non-code descriptions, the supplied requirements
reconnaissance, our packets and tshark as a black-box decoder informed this
codec. [PROVENANCE.md](PROVENANCE.md) records the public sources, their use and
known errata. No paywalled standards text is reproduced. Apache-2.0 terms are
in [LICENSE](LICENSE).
