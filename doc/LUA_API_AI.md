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

All matching uses OpenCV `TM_SQDIFF_NORMED` → **lower `score` = better** (0 = perfect).

```
vision.MatchTemplate(screenPath, tplPath, minScale?:num=0.5, maxScale?:num=1.5, step?:num=0.05)
    -> {x,y,width,height,scale,score,centerX,centerY} | nil,err
    Multi-scale template match.

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
| `md`/`mu` | mouse down/up | `btn`,`x`,`y` |
| `mw` | wheel | `roll` |
| `gp` | gamepad frame | `gp_b`,`gp_lt`,`gp_rt`,`gp_lx`,`gp_ly`,`gp_rx`,`gp_ry` |

`gp` fields map 1:1 to the Lua gamepad API:
`gp_b`→`buttons`, `gp_lt`→`lt`, `gp_rt`→`rt`, `gp_lx/gp_ly`→`lx/ly`, `gp_rx/gp_ry`→`rx/ry`.

## CLI

```
GoRunner.exe <file.lua>          run Lua
GoRunner.exe <file.script>       replay (PgUp start/pause, PgDn stop)
GoRunner.exe -f <file.script>    replay immediately, no key wait
GoRunner.exe -t <scale> <file>   time scaling (1.5 = 1.5x faster)
GoRunner.exe                     no args -> interactive RECORD mode
GoPacker.exe <file.lua|.script>            -> single-file EXE
GoPacker.exe <file.lua|.script> -unpak     -> bin/run/<name>/ plaintext debug folder
```
Packed EXE accepts the same `-f` / `-t` flags (script payload only).

## INVARIANTS / TRAPS (for generated code)

1. `CheckPgUpTrigger`/`CheckPgDnTrigger` must be polled ~20ms in a loop; they are edge-triggered.
2. Always `GamepadReset()` before pausing/exiting — sticks are state, not events.
3. Gamepad needs ViGEmBus; physical pad on slot 0 hides the virtual pad (slot 1) from slot-0-only games.
4. Vision matchers need a file path; `CaptureClient` first. Lower `score` = better match.
5. `AssetPath` never verifies existence; empty `Asset/` produces no `.pak`.
6. GoFSM `ctx` is read-only; keep state in upvalues.
7. `.script` is JSON data — never execute it as Lua (GoPacker routes by extension).
8. No built-in loop/wait helpers: write `while` + `input.Sleep(ms)` yourself.
9. No networking/filesystem Lua API beyond `AssetPath` + standard Lua `io`/`os`.
10. `GoRecord` does not exist; there is no Lua recording API.
