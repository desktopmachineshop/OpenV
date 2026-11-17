# Site media: the videos and screenshots of openv.app

Every video and screenshot on the site is made here, from the running app,
so a change to the app's screens means running a command again rather than
recording by hand. The plan is OpenV's `docs/plans/website-refresh.md`.

## What a video is

A folder under `videos/` holding `video.json`:

```json
{
  "title": "Requirements and the project tree",
  "description": "One or two sentences for the page and the upload.",
  "viewport": "desktop",
  "steps": [
    {
      "id": "intro",
      "do": [["goto", "/projects/{Desktop CNC Mill}/requirements"], ["shot", "requirements-table"]],
      "say": "The line said over this step."
    }
  ]
}
```

Each step has what to do on screen (`do`) and what to say over it (`say`).
A step lasts as long as its line, so the picture waits for the voice.
`{Project name}` in a path is looked up when the video is recorded, since
project ids differ from one machine to the next. `viewport` is `desktop`
(1280 by 720, scaled to 1920 by 1080) or `phone` (an iPhone 13).

The actions: `goto`, `click`, `dblclick`, `hover`, `fill`, `type` (one
key at a time, as a person types), `press`, `select`, `scroll` (bring an
element into view), `wheel` (scroll by pixels), `see` (wait until an
element shows), `wait` (milliseconds), and `shot` (save a screenshot for
the site). Selectors are Playwright's: `text=...`, `button:has-text('...')`
or CSS.

## Making one

```bash
export SITE_MEDIA_URL=http://localhost:4173 SITE_MEDIA_EMAIL=... SITE_MEDIA_PASSWORD=...
export SITE_MEDIA_KOKORO=/path/to/kokoro   # kokoro-v1.0.onnx and voices-v1.0.bin
python3 site_media.py check requirements   # run the steps, record nothing: finds a moved button
python3 site_media.py build requirements   # voice, record, cut and join
```

The account signs in to an OpenV with the sample programme loaded
(`examples/desktop-cnc-mill/load.py`). Everything ends up in
`build/out/`: `<video>.mp4`, a poster `<video>.jpg`, captions
`<video>.vtt`, `<video>.json` (title, description, length and each step's
line with its start and end, for the transcript on the page and the
upload's chapters) and `screenshots/*.webp`, with a 1280-wide `-1280.webp`
copy of each shot wider than that for phones.

The voice is Kokoro, an open speech model that runs offline
(`pip install kokoro-onnx soundfile`; the model files are on the
kokoro-onnx project's releases page). The voice itself is set once, in
`VOICE` at the top of `site_media.py`, so every video sounds the same.

## Changing one

Every step's picture and voice are kept in
`build/<video>/steps/NN-<id>/` as `clip.mp4` and `voice.wav`, so a change
touches only what it has to.

- **Reword a line:** edit `say`, then `build <video> --only <step id>`.
  Only that line is said again, and its clip is cut again from the
  recording. If the new line is longer than the picture, the last frame
  holds; record again if that shows.
- **The screen changed:** `check <video>` names the first step whose
  element is gone. Fix its `do`, then `build <video> --record`.
- **Use a person's voice for a line:** put `overrides/NN-<id>/voice.wav`
  in the video's folder and build again. The same for a picture recorded
  by hand: `overrides/NN-<id>/clip.mp4`.
- **Cut it in an editor:** `export <video> <folder>` copies every step's
  clip and voice with `timeline.json`, which gives each step's start and
  end in the finished video.

The build fails if a video passes 50 MB, where GitHub starts to warn; the
files go in git for now (the plan's section 5).
