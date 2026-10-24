# iec104go

Clean-room, Apache-2.0 IEC 60870-5-104 stack, with no dependencies outside
Go's standard library. Module: `github.com/tonylturner/iec104go`.

APCI, exact-boundary stream reading, ASDU envelopes, 54 process/system/parameter/file
types, CP56Time2a and CP16Time2a, supervised TCP sessions, client/server roles,
and initiating procedure helpers are implemented (phases 1a/1b/1c). Service ports
and differential conformance runs remain separate phases. This is not a claim
of independently certified IEC conformance.

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
_ = wire // raw encoding; Session.Send(a) normally owns sequencing/transmission
```

- `Frame.MarshalBinary` / `DecodeFrame` operate on one complete APDU.
  `NewAPDUReader(io.Reader).ReadFrame()` handles fragmented and coalesced
  streams without read-ahead. No scanning past malformed data. Caller owns
  deadlines, cancellation and synchronization; raw codec calls start no goroutines.
  I-frame payload structure is checked separately by `DecodeASDU`.
- `DecodeASDU(bytes, params)` returns `Message`, concretely `*ASDU` or
  `*RawASDU`. Both expose `GetHeader()` and `MarshalBinary(params)`.
  `errors.Is(err, ErrMalformed)` identifies structural errors. Unsupported
  types yield a successful `RawASDU` whose `DecodeError` wraps
  `ErrUnsupportedType`; its complete header and opaque payload are retained.
  The application, not the codec, decides rejection precedence and COT 44–47.
- `ASDU{Header, Objects}` supports both SQ modes. `Object{Address, Element}`
  exposes every address even with SQ=1. Typed ASDUs derive count from Objects;
  raw ASDUs use Header.Count. Generic SQ requires contiguous, nonoverflowing addresses; directory126
  instead repeats the shared directory IOA on every Object.
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
  own their object slices and segment bytes; Append also copies segment data.
  Decoded messages own all bytes. Callers must not
  mutate shared messages while another goroutine encodes them.
- `CP56Time2a` keeps raw fields and flags even for invalid calendars.
  `Time()` interprets the year as 2000–2099 UTC and returns false for invalid
  calendars, independently of the IV bit. `CP56FromTime` emits UTC, DOW=0,
  SU=0, year modulo 100; precision is milliseconds. `RES1` preserves minute
  bit 6 (also called GEN in newer profiles), without interpreting it.
- `Params` must be explicit; its zero value is invalid. Optional smaller
  widths are a codec convenience, not the selected IEC 104 wire profile.

See [PICS.md](PICS.md) for type/COT coverage and profile decisions.

## Sessions, clients and servers

```go
opts := iec104go.DefaultOptions()
client, err := iec104go.Dial(ctx, "127.0.0.1:2404", opts,
    func(callbackCtx context.Context, e iec104go.Event) {
        // e.ConnID is never reused; e.ASDU may be retained after return.
        // e.Session.RemoteAddr() / LocalAddr() identify this connection.
        // Honor callbackCtx cancellation if doing blocking application work.
    })
if err != nil { return err }
peer, local := client.RemoteAddr(), client.LocalAddr()
_, _ = peer, local // also available through Event.Session on the server
operationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
err = client.StartDT(operationCtx)
var responses []iec104go.Message
if err == nil {
    responses, err = client.Interrogate(operationCtx, 1, 0, 20)
}
_ = responses
shutdownCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
defer finish()
closeErr := client.Close(shutdownCtx) // cleanup on failure too, including callbacks
if err != nil { return err }
return closeErr
```

- `Dial` accepts a context, `HOST:PORT`, options and handler. An optional
  `LocalAddress` binds the source. t0 bounds TCP establishment; STARTDT is
  explicit. `NewSession(ctx, net.Conn, role, opts, handler)` supports established
  transports and deterministic `net.Pipe` tests. There is no reconnect loop.
- `Session.RemoteAddr()` / `LocalAddr()` return immutable `net.Addr` snapshots
  captured before Opened, safe concurrently, in callbacks and after Close.
  Access them through `Event.Session` on either side, or directly on `Client`.
  Use `Network()` / `String()`, not concrete transport address type assertions.
  Nil means the transport supplies no address; `net.Pipe` reports `"pipe"`.
- `NewServer(opts, handler)` returns a server. `Serve(ctx, listener)` owns the
  listener on success. Call `Shutdown(ctx)` to close it and join every session.
  The default ninth simultaneous connection is accepted, closed and logged.
  `Server.Send(connID, message)` never reroutes a retired identity.
- `Session.Send` and `Server.Broadcast` never wait for k, the network or a
  handler. They copy encoded data into bounded queues; `ErrQueueFull` means no
  admission. Broadcast snapshots started recipients once, sorts by ConnID and
  returns an error per recipient. Admission is not a delivery guarantee after
  transport failure; inspect the final `Closed` reason.
  A handler sending an image larger than `SendQueue + k` ASDUs can get
  `ErrQueueFull` (and a smaller burst can too if capacity is already occupied).
  Pack information objects into ASDUs with `Builder`, or size `SendQueue` for
  the image; successful nonblocking admission is not unlimited buffering.
- One dedicated callback goroutine per connection delivers `Opened`, `Started`,
  `ASDUReceived`, `Stopped`, `Closed` in protocol order. Closed occurs exactly
  once and last. Callbacks do not stall APCI reads, writes, ACKs or timers.
  `Event.Session` is already initialized even if Opened runs before the
  constructor returns; use it instead of capturing an unpublished pointer.
  Never call `Close` or `Shutdown` from a callback: they wait for that same
  callback to finish. Use `RequestClose()` there, not a blocking lifecycle call.
- Queues default to 64 sends and 256 pending callback events. Callback overflow
  closes with `ErrEventQueueFull`; the triggering event and terminal Closed
  have reserved slots and are still delivered. Increase `EventQueue` for larger
  images/backlogs. A transaction uses the same response-count bound and reports
  explicit overflow rather than hiding missing responses.
- `Close(ctx)`, `Shutdown(ctx)` and `Done()` include callback completion.
  Context cancellation interrupts transport I/O. Go cannot kill application
  callbacks: a handler ignoring cancellation can cause Close/Shutdown to return
  the caller's context error until it returns; after it returns cleanup joins.
  Taps and custom loggers must also return promptly.
- `APCI` defaults to k=12/w=8, t0/t1/t2/t3=30/15/10/20 seconds. Only individual
  ranges are checked. `Clock`/`Timer` use absolute deadlines to avoid races
  between a fake-clock advance and timer registration;
  TCP dial t0 uses `net.Dialer`'s context/timeout.
- `StartDT(ctx)`/`StopDT(ctx)` await t1-supervised confirmations. Stop freezes
  admission and drains already admitted data before its act/con; no accepted
  object is silently discarded. Counters survive stop/start. `Err()` reports
  peer FIN, I/O failure, `ErrT1`, `ErrProtocol` or local `ErrClosed`.
- `Tap` callbacks are serialized per connection: Receive after complete decode,
  Transmit immediately before the single Write attempt. Short writes close,
  never retry. Tap buffers are owned copies; no shared mutable transport buffer.
- `Mirror(message, cause, negative)` copies typed or opaque data without choosing
  application rejection policy. No COT 44–47 answer is generated automatically.
- Client helpers: `Interrogate`, `CounterInterrogate`, `Read`, `SyncClock`,
  `Test`, `Reset`, `Command`, `SelectExecute`. Each requires a caller deadline,
  returns observed responses on success/failure, never retries, and allows one
  transaction per connection (`ErrProcedureBusy` otherwise). They observe copies
  independently of callbacks. GI/counters and execute require CON then TERM;
  selection/clock/test/reset require CON only; read requires requested data.
  Bitstring commands support direct execute only, because they have no SE bit.
  Each request leg becomes eligible at the send Tap/single-Write boundary;
  earlier matching responses are ignored, including for both SBO legs.
  Responses racing Write completion are buffered, but cannot report success
  until the complete APDU Write succeeds. Recorded response overflow takes
  precedence over buffered success, and finalization atomically detaches/fences.
  An abandoned/overflowed/unordered exchange fences later helpers with
  `ErrProcedureUncertain`: reconnect before another helper, since the wire has
  no transaction ID to distinguish late identical echoes. This does not choose
  application disconnect policy or prevent manual Send/callback handling.
  Station acceptance, SBO ownership/expiry and actual field execution are
  application policy, not automatically implemented by this library.

## Transparent file transfer

Types120–127 have typed values: `FileReady`, `SectionReady`, `FileCall`,
`FileLast`, `FileACK`, `FileSegment`, `FileDirectory`, `QueryLog`. NOS is
**one octet**. Type125 is variable-length; `TypeID.Supported()` covers it,
while `ElementSize()` intentionally reports fixed sizes only. Directory126
requires SQ1 with a shared IOA; the other seven types require SQ0/count1.
All reserved fields, unused zero lengths and raw times remain codec-lossless.

- `client.Directory(ctx, FileKey{CommonAddress:1, Originator:0, Address:100})`
  receives entries through SOF.LFD, across ASDU boundaries.
- `client.DownloadFile(ctx, FileKey{CommonAddress:1, Originator:0,
  Address:100, Name:9})` selects/calls, receives sections, checks lengths and
  section/file sums modulo256, and writes the final ACK before success.
  The result is `File`, with ordered `FileSection{Number, Data}` entries in
  `Sections`; on failure any returned content is partial. No filesystem or
  payload-format policy is built into the library. Both calls require a
  caller deadline and share the existing one-transaction-per-link exclusion.
- Implement `FileProvider.Directory(ctx, key) ([]FileDirectory,error)` and
  `Open(ctx, key) (File,error)`. Provider results transfer slice ownership;
  honor cancellation and allow different connections to call concurrently.
  Return `ErrFileNotFound` for missing content. Then construct
  `files, err := NewFileServer(provider)` and pass `files.Handler` to
  `NewServer`/`NewSession`, or compose it with your normal event handler:
  `files.Handler(ctx, e); applicationHandler(ctx, e)`. The optional handler
  isolates provider work in a bounded worker, and Closed cancels/joins it.
  Never block on Close/Shutdown from either callback/provider.
- `Options.Files` defaults: application timeout30s, section checksum retries0,
  local admission polling10ms, max file16777215 bytes, directory4096 entries.
  Zero durations/limits select defaults; retries range0..10. These are **profile
  choices**, not104 t1/t2/t3. Only checksum-driven section recalls are retryable
  (configure both peers); control timeouts/whole files never silently retry.
- Empty directories/zero-content files/sections are rejected, not represented
  with invented terminators. Delete/select-section/deactivate-section/uploads
  are not implemented by the serving helper. QueryLog127's inclusive timestamps
  and zero sentinels are codec-supported, but the transparent handler rejects
  positive log requests with FRQ not-ready. Route127 elsewhere if implementing
  archives; no positive QueryLog service or upload interoperability is claimed.
- Any admitted file failure fences subsequent helpers until reconnect and
  best-effort sends deactivation. Inspect `FileResponseError`,
  `ErrFileChecksum`, `ErrFileAborted`, and the existing procedure errors.
  File response buffers drain as consumed; a large file need not fit entirely
  in EventQueue, but bursts/callbacks/worker inboxes are still bounded. Queue
  overflow is explicit. `Send` itself remains nonblocking.

[PICS F1–F20](PICS.md#file-transfer-phase-1c) document all conservative choices
and their evidence boundaries; **peer agreement is required**, not assumed.

## Validation

From this directory (Go downloads the pinned toolchain if necessary):

```sh
gofmt -l .                       # must print nothing
go vet ./...
go test -race -count=1 -timeout 300s ./...
go test -race -count=20 -run 'Session|Client|Server|Procedure' -timeout 300s ./...
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
stack. [PROVENANCE.md](PROVENANCE.md) records the public sources, their use and
known errata. No paywalled standards text is reproduced. Apache-2.0 terms are
in [LICENSE](LICENSE).
