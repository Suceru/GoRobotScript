# Lua API — AI Reference (GoRobotScript)

Machine-oriented reference. Exact signatures, semantics, failure modes.
Human-oriented version with examples: [`LUA_API.md`](./LUA_API.md)

```
RUNTIME : bin/GoRunner.exe <script.lua>          (also runs .script replay)
ENGINES : .lua → Lua engine (GoLua) | .script → replay engine (GoRunner)
LOADER  : local m = require("<ModuleName>")
SCOPE   : one Lua state per run; no persistence between runs
```

## MODULES

| Module | Status |
| :--- | :--- |
| `GoInput` | registered |
| `GoVision` | registered |
| `GoFSM` | registered |
| `SuKey` | alias, identical to `GoInput` |
| `SuScreen` | alias, identical to `GoVision` |
| `CallGo` | registered, legacy only |
| `GoRecord` | **NOT registered — does not exist** |

## GLOBALS

```
AssetPath(sub?:string="") -> string
    Pure path join: <script_dir>/Asset/<sub>, returned as an ABSOLUTE path with
    forward slashes. Does NOT verify existence.
    If app has no .pak, the Asset/ dir is never created.

AppInfo() -> { name:string, version:string }
    name = base name of the SCRIPT'S DIRECTORY (not the exe), version always "1.0.0"

PackageInfo(name?:string="App.exe", version?:string="1.0.0", mode?:string) -> {name,version}
PackageMode(mode:string) -> (no return)
    NOTE: these two have NO runtime effect. They are parsed as TEXT by
    GoPacker at pack time (regex) to set output name / folder mode.
    Equivalent comments: "-- @pack_mode folder", "-- @external_dll"
```

## GoInput

### Keyboard
```
input.KeyTap(key:string, ...mods:string) -> nil
input.KeyTap({"c","ctrl"})                -> nil    -- table form: [1]=key, rest=mods
input.TypeStr(text:string)                -> nil
```
Key names follow robotgo: `"a".."z"`, `"0".."9"`, `"f1".."f12"`, `"enter"`, `"esc"`,
`"tab"`, `"space"`, `"backspace"`, `"delete"`, `"up"/"down"/"left"/"right"`,
`"pageup"`, `"pagedown"`, `"ctrl"`, `"alt"`, `"shift"`, `"cmd"`.

### Mouse
```
input.Move(x:int, y:int)                          -> nil          -- absolute screen coords
input.Click(button?:string="left", double?:bool=false) -> nil      -- "left"|"right"|"center"
input.ClickClient(title:string, cx:int, cy:int)   -> bool         -- window message click, client-area coords, background, no cursor move
input.Sleep(ms:int)                               -> nil
```

### Global hotkeys (polling, edge-triggered)
```
input.CheckPgUpTrigger() -> bool      -- true once per physical press (rising edge)
input.CheckPgDnTrigger() -> bool
```
```
SEMANTICS: each call refreshes internal last-state. MUST be polled in a loop
           (~20ms). Long sleeps between calls swallow presses.
           Backed by GetAsyncKeyState → works regardless of window focus,
           including fullscreen games.
```

### Sounds (all -> nil)
```
input.SoundStart() | input.SoundPause() | input.SoundResume() | input.SoundStop()
```

### Virtual gamepad (Xbox 360 via ViGEmBus)
```
input.Gamepad(lx:int, ly?:int)  -> bool | false,err
input.Gamepad(tbl)              -> bool | false,err
input.GamepadReset()            -> bool
input.GamepadSlots()            -> {int,...}          -- occupied XInput slots, e.g. {0,1}
input.GamepadClose()            -> nil
input.GamepadLeftStick(lx,ly)   -> bool | false,err   -- legacy
input.GamepadRightStick(rx,ry)  -> bool | false,err   -- legacy
```
```
tbl fields (ALL OPTIONAL — omitted field KEEPS its previous value):
  lx, ly : int  -32768..32767   left stick  (movement)
  rx, ry : int  -32768..32767   right stick (camera)
  lt, rt : int  0..255          triggers
  buttons: int|string|table     see BTN below
      int   -> raw mask
      "A"   -> single name
      {"A","LB"} -> name array

STATE NOT EVENT: values persist until changed again or GamepadReset().
Combining: input.Gamepad{buttons = input.BTN.A + input.BTN.LB, rt=255, rx=20000}
```
```
input.BTN constants (add them to combine):
  A=0x1000  B=0x2000  X=0x4000  Y=0x8000
  LB=0x0100 RB=0x0200 L1=0x0100 R1=0x0200
  LS=0x0040 RS=0x0080 L3=0x0040 R3=0x0080
  START=0x0010 BACK=0x0020
  UP=0x0001 DOWN=0x0002 LEFT=0x0004 RIGHT=0x0008
```
```
SLOT RULE: virtual pad takes the LOWEST FREE XInput slot. If a physical pad
           already holds slot 0, the virtual pad lands on slot 1 and games that
           read only slot 0 will ignore it. Diagnose with GamepadSlots().
           Gamepad() auto-creates the pad on first call.
```

## GoVision

Lua-side matching uses OpenCV `TM_SQDIFF_NORMED` → **lower `score` = better** (0 = perfect).
Every result table ALSO carries `percent` = similarity in `0..100` (**higher = better**), which is the
same percentage scale as the replay-side vision threshold `-vs`; prefer `percent` for decisions.

```
vision.MatchTemplate(screenPath, tplPath, minScale?:num=0.5, maxScale?:num=1.5, step?:num=0.05)
    -> {x,y,width,height,scale,score,percent,centerX,centerY} | nil,err
    Multi-scale template match. score = raw SQDIFF error (lower better), percent = 0..100 similarity.

vision.MatchColorGrid(screenPath, tplPath, rows?:int=3, cols?:int=3,
                      minScale?:num=0.5, maxScale?:num=1.5, step?:num=0.05)
    -> same fields as MatchTemplate | nil,err
    Pools both images to rows x cols color grid before matching.

vision.GetPixel(screenPath, x:int, y:int) -> r,g,b | nil,err      -- THREE return values
vision.CheckMultiColor(screenPath, points, tolerance?:int=20) -> matched:bool, ratio:num | false,err
    points = { {x=,y=,r=,g=,b=}, ... }
    image is loaded FIRST (bad path -> false,err even if points is empty)
    matched = ALL points within tolerance; ratio = matched/total (0..1)
    empty points table -> true, 1.0 ; out-of-bounds points are skipped;
    if no valid point remains -> false,err

vision.CaptureClient(title:string, savePath:string) -> bool | false,err
    Captures the CLIENT AREA of the first window whose title contains `title`; writes PNG.

vision.CaptureScreen(key:string, {x,y,w,h})  -> nil    -- caches bitmap in memory, silent failure
vision.SaveBitmap(key:string, destPath:string) -> nil  -- silent failure
```
```
NOTE: all vision matchers take a FILE PATH, not a gocv.Mat. Capture first
      (CaptureClient) then match. Failing to load an image yields nil,err.
```

## GoFSM

```
local fsm = FSM.new()
fsm:addState(name:string, fn:function) -> nil
fsm:setInitial(name:string)            -> nil
fsm:step()        -> stateName:string | nil,err      -- returns state AFTER the step
fsm:getCurrent()  -> stateName:string
```
```
State fn signature: function(ctx) -> nextStateName|nil end
  return nil or "" -> state unchanged
  return unregistered name -> next step() errors

ctx IS A READ-ONLY SNAPSHOT rebuilt on every call; it contains only
string/int/float/bool fields. Mutating ctx does NOT persist.
Use Lua upvalues for state.
```

## CallGo (legacy)

```
CallGo.showalert(title:string, msg:string) -> bool
CallGo.keyLog() -> nil     -- starts a 3D recorder; blocks and grabs input
```

## RETURN / ERROR CONVENTION

```
success -> business value only (table | bool | numbers), NO error slot
failure -> nil (or false) AND error string as 2nd return value

VOID = exactly 0 return values (NOT nil):
    KeyTap TypeStr Move Click Sleep SoundStart SoundPause SoundResume SoundStop
    CaptureScreen SaveBitmap GamepadClose
  Consequences:
    local x = input.Move(0,0)          -- legal, x == nil
    print(tostring(input.Move(0,0)))   -- ERROR: bad argument #1 to tostring (value expected)
  These give NO success/failure feedback. Use ClickClient (returns bool) when
  you need confirmation.

ARITY (verified): Sleep/Move -> 0 values ; GamepadReset/GamepadSlots/CheckPgUp -> 1 ;
                  GetPixel/MatchTemplate failure -> 2 (nil, err)
```

## RECORDED .script FORMAT (JSON Lines, one action per line)

```
{"dt":3,"op":"init","x":1122,"y":596}
{"dt":1455,"op":"gp","gp_ly":2258}
{"dt":15,"op":"rmv","dx":-3,"dy":1}
```
| field | type | meaning |
| :--- | :--- | :--- |
| `dt` | int | ms since previous frame (replay throttle; scaled by `-t`) |
| `op` | string | action kind |

| op | meaning | fields |
| :--- | :--- | :--- |
| `init` | anchor start cursor | `x`,`y` |
| `mv` | absolute cursor move; **replay ignores its dx/dy** | `x`,`y`,`dx`,`dy` |
| `rmv` | 3D/VR hardware relative delta (Raw Input) | `dx`,`dy` |
| `kd`/`ku` | key down/up | `key` |
| `md`/`mu` | mouse down/up (keypoint when `vi` present) | `btn`,`x`,`y`, vision sample fields |
| `mw` | wheel | `roll` |
| `gp` | gamepad frame | `gp_b`,`gp_lt`,`gp_rt`,`gp_lx`,`gp_ly`,`gp_rx`,`gp_ry` |
| `vision` | vision on/off marker line (`dt` always 0, no input) | `von`,`vsl`,`vm`,`vz` |
| `vslot` | vision preset-slot change marker line | `vsl`,`vm`,`vz` |

Vision sample fields on `md`/`mu` frames (only present when vision was ON at that moment):

| field | type | meaning |
| :--- | :--- | :--- |
| `vi` | string | main sample name WITHOUT extension (`md-left-001` -> `<name>.png`) |
| `vx`,`vy` | int | cursor offset inside the main sample. Sample size is ALWAYS `vz`x`vz`: near a screen edge the whole patch shifts inward instead of being clipped, so the offset is then != size/2 |
| `vm` | string | `image` (MatchTemplate) or `color` (ColorConvolutionMatch) |
| `vz` | int | sample edge: 32 / 64 / 128 |

Exactly ONE image per click (no extra sample files). Sample dir convention:
`<scriptDir>/<scriptBase>.vision/`. Naming `<op>-<btn>-NNN`, 3 digits, counter per op+btn pair.

REPLAY RECOGNITION PIPELINE (whole script read into memory up front; .script format unchanged),
tried cheapest-first / smallest-region-first:
1. PAIR CONCURRENT SCAN: if the mousedown->mouseup displacement is <= `-vpair` (default 32px) both
   samples are searched CONCURRENTLY inside the same fast region; the first success wins, the other
   keypoint is derived by the recorded displacement and the losing scan is cancelled (never blocked
   by the slow side). Falls back to the normal path when no fast region exists.
2. PAIR CONSISTENCY: a click pair (mousedown->mouseup displacement <= `-vpair`) must stay at ONE spot.
   The later sample is only rechecked in the tiny "displacement + tolerance" box; if that box has no
   hit, the position is derived from the winning side by the recorded displacement - it never widens
   the search to find another location, which would turn one click into a drag.
3. FAST REGION CACHE: after a keypoint is recognized, a region of side `sampleSize * cacheFactor`
   (default 4x) is cached around it; the next keypoint is searched there first.
4. REGION CONFIDENCE GATE (generic, no layout assumptions): a restricted region only ever yields a
   LOCAL best - a look-alike target may sit inside it while the real one is outside. So any region
   result (pair box, fast region, expanded region) must reach `-vconf` (default 97%);
   otherwise the search RANGE GROWS step by step (tiny box -> fast region -> expanded region
   `-vcache`x3 -> full screen). Only the full-screen result is the GLOBAL best, so it only needs the
   `-vs` threshold. `-vconf 0` (or negative) disables the gate. This is what prevents an early mark
   from latching onto a similar neighbour and warping the path the wrong way.
5. BACKGROUND PREFETCH: a lookahead window (default 5 keypoints) is recognized ahead of time in
   goroutines; same region ladder as above.
   Full-screen searches are capped at 2 concurrent; screen captures are serialized.
6. RECHECK AT USE: when playback actually reaches a keypoint, the prefetched candidate is
   re-verified against the LIVE screen in a small region; pass => use it (accelerated),
   fail => fall back to standard recognition.
7. In-flight prefetches outside the sliding window are cancelled.

CLICK POSITION GUARANTEE (playback main loop): a keypoint is re-verified at its own frame and may be
re-located there, therefore:
  * a keypoint frame's coordinates ALWAYS come from that keypoint's final confirmed position, never
    from a segment transform that may still hold the stale early mark;
  * whenever a position changes, every segment transform referencing that keypoint is invalidated and
    rebuilt (including the trailing path after the last keypoint) - no "click then jump back".
A keypoint also moves the cursor to its currently known position BEFORE the recorded dwell, so the UI
sees the cursor in place (same hover state as during recording); whenever the click position still has
to be adjusted at the click instant, playback waits `-vsettle` (default 30ms) before pressing/releasing.
Measured live (2560x1440, 18 keypoints = 9 click pairs): 2 full-screen searches,
pair reuse 1, restricted-region hits 15/26 (4 widened because below the confidence gate),
prefetch 16/16, recheck 33 passed / 0 re-located.
Set `GOROBOT_VISION_DEBUG=1` to log every pipeline decision.

REPLAY ALIGNMENT (keypoints): each `md`/`mu` with `vi` is a keypoint; it is re-located on
the live screen, and the path between two keypoints is mapped by a **similarity transform**
(rotate + uniform scale + translate) that maps rawStart->matchedStart and rawEnd->matchedEnd
exactly, so segments are continuous (no jumps). The transform is NOT scale-clamped (clamping would
break endpoint alignment). End keypoint of one round = start keypoint of the next. A
`{"op":"vision","von":false}` marker breaks the chain: frames while OFF replay at raw coordinates,
and the next ON starts a fresh chain.

SEGMENT DEGRADATION LADDER (prefer no alignment over jumping):
  * both ends matched, rotation within `-vrot`, both HIGH-CONFIDENCE (score >= `-vconf` default 97)
    => rotate/stretch, both endpoints aligned EXACTLY (any displacement ratio is accepted);
  * both ends matched but at least one only marginal (< `-vconf`) => rotate/stretch only if the
    recorded/matched displacement ratio is inside [0.5, 2.0], otherwise pure translation;
  * rotation beyond `-vrot` => pure translation (better-scoring end);
  * both keypoints recorded at the same spot (down/up of one click) => transform undefined, translation;
  * one end matched => pure translation from that end (path shape preserved);
  * no end matched => replay at raw coordinates.
  Reason is printed after each segment line (`<- 放弃旋转/拉伸：...`).

DISPLACEMENT-RATIO RULE: when both endpoints are HIGH-CONFIDENCE matches (score >= `-vconf`,
default 97), the ratio between the recorded and the matched displacement is accepted as-is, no matter
how far it is from 1.0 - in a UI whose content is re-randomized every round, two keypoints that were
300px apart while recording may legitimately be 100px apart (0.45x) or 240px apart (2.1x) now.
Rejecting such a ratio degrades the segment to "pure translation (start trusted)" = replay at the OLD
recorded coordinates, which makes the cursor travel AWAY from the next click target and then jump to
the correct position at the click. The geometric `[0.5, 2.0]` range is only used when at least one
endpoint is a marginal match. A degenerate segment (both keypoints recorded at the same spot = the
down/up of one click) has no defined transform and always uses pure translation.

## CLI

```
GoRunner.exe <file.lua>          run Lua
GoRunner.exe <file.script>       replay (PgUp start/pause, PgDn stop)
GoRunner.exe -f <file.script>    replay immediately, no key wait
GoRunner.exe -t <scale> <file>   time scaling (1.5 = 1.5x faster)
GoRunner.exe -vs <pct> <file>    similarity threshold in percent (default 85, higher = stricter).
GoRunner.exe -vblur <k> <file>   pre-match Gaussian blur kernel (default 3) - tolerant to blur/sharpening
GoRunner.exe -vmin/-vmax <f>    scale search range (default 0.9/1.1); widen when game resolution differs
GoRunner.exe -vrot <deg> <file>  max rotation for path warp (default 180 = unlimited; 360 is the same full
                                circle, since a signed angle lives in (-180,+180]). Keep unlimited when
                                the UI re-randomizes positions each round; set ~15 for fixed layouts.
GoRunner.exe -vfast <n> <file>   prefetch window: recognize n keypoints ahead (0 disables)
GoRunner.exe -vcache <n> <file>  fast region side = sample edge * n (default 4)
GoRunner.exe -vtol <px> <file>   recheck position tolerance (default 6)
GoRunner.exe -vpair <px> <file>  pair-reuse threshold: md->mu displacement to reuse (default 32)
GoRunner.exe -vconf <pct> <file> confidence gate for restricted-region results (default 97; below it the
                                search range grows up to full screen; 0/negative disables the gate)
GoRunner.exe -vsettle <ms> <file> wait before click/release when the cursor was just corrected at that
                                instant (default 30; 0 disables). Lets the UI register the cursor position.
GoRunner.exe -vdir <dir> <file>  override vision sample dir
GoRunner.exe                     no args -> interactive RECORD mode
GoPacker.exe <file.lua|.script>            -> single-file EXE
GoPacker.exe <file.lua|.script> -unpak     -> bin/run/<name>/ plaintext debug folder
```
Packed EXE (script payload) accepts ONLY `-f` (play to end), `-t <scale>` (time scaling) and
`-vt <pct>` (vision similarity threshold, i.e. the packed equivalent of `-vs`; other vision flags
are not forwarded yet). A Lua payload takes no flags.
A `.script` pack embeds `<scriptBase>.vision/` as a zip in the payload (V3) and extracts it to
`%LOCALAPPDATA%\GoRobotScript\vision_<hash>\`; `-unpak` copies the folder next to the script instead.

RECORDING HOTKEYS: `PgUp` start/pause-resume, `PgDn` end+save (also exits vision),
`Pause` toggles vision on/off, `Home` SHORT press (<450ms) = next preset slot,
`Home` LONG press (>=450ms) = toggle the cursor drawing box, `End` = previous preset slot.
6 preset slots, grouped by matcher: slots 1-3 = image 32/64/128, slots 4-6 = color 32/64/128.
Slot state is NOT persisted across rounds.
The drawing box is a red 3px hollow frame following the cursor. Its **inner hole equals the
capture area exactly** (the frame is drawn OUTSIDE the patch), so red pixels never leak into
the saved template; the window is `patch + 2*line` in size. It is click-through, never takes
focus, does not change the cursor shape, and is destroyed on `Pause`-off / `PgDn`. It is a
recording-time visual aid only: nothing about it is written to the .script nor used during replay.
Status text (fixed column, in-place refresh): `识图关闭` / `识图开启-图像32*32p` / `识图开启-颜色128*128p`.

The .script file is opened READ-ONLY during replay: the whole file is loaded into memory,
keypoint aggregation and path adjustment happen on in-memory copies only, and the original
file is never rewritten (verified by regression test over hash + mtime).

## INVARIANTS / TRAPS (for generated code)

1. `CheckPgUpTrigger`/`CheckPgDnTrigger` must be polled ~20ms in a loop; they are edge-triggered.
2. Always `GamepadReset()` before pausing/exiting — sticks are state, not events.
3. Gamepad needs ViGEmBus; physical pad on slot 0 hides the virtual pad (slot 1) from slot-0-only games.
4. Vision matchers need a file path; `CaptureClient` first. Lower `score` = better match;
   `percent` (0..100, higher = better) is the same scale as the replay-side `-vs` threshold.
5. `AssetPath` never verifies existence; empty `Asset/` produces no `.pak`.
6. GoFSM `ctx` is read-only; keep state in upvalues.
7. `.script` is JSON data — never execute it as Lua (GoPacker routes by extension).
8. No built-in loop/wait helpers: write `while` + `input.Sleep(ms)` yourself.
9. No networking/filesystem Lua API beyond `AssetPath` + standard Lua `io`/`os`.
10. `GoRecord` does not exist; there is no Lua recording API.
11. Vision **marker lines** (`op` = `vision`/`vslot`) carry no input: never treat them as mouse actions.
12. Vision matching is unsafe on low-texture samples — a flat patch (solid UI color) has no unique
    location and scores near 0 anywhere. Sample the click point on textured content.
13. Replay vision only relocates keypoints (`md`/`mu` with `vi`); plain `mv` paths are warped by the
    enclosing segment transform, never matched individually.
14. Replay never writes to the `.script` file; alignment is an in-memory aggregation pass. Do not
    assume the file contains adjusted coordinates after a run.
