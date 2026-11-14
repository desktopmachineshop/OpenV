#!/usr/bin/env python3
"""Make the site's videos and screenshots from the running app (README.md).

    site_media.py check   <video>             run the steps without recording
    site_media.py build   <video> [--record]  voice, record if asked or needed, cut, join
    site_media.py build   <video> --only <step>
    site_media.py export  <video> <dir>       the pieces, for editing by hand
    site_media.py list

A video is a folder under videos/ holding video.json: its title and its
steps, each with what to do on screen and the line to say over it. The
build keeps every step's picture and voice under build/<video>/steps, so
one step can be changed without redoing the rest.

Settings come from the environment:
  SITE_MEDIA_URL        the app to record, e.g. http://localhost:4173
  SITE_MEDIA_EMAIL      the account to sign in as
  SITE_MEDIA_PASSWORD
  SITE_MEDIA_KOKORO     the folder holding kokoro-v1.0.onnx and voices-v1.0.bin
  SITE_MEDIA_OUT        where finished files go (default build/out)
"""

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
VIDEOS = os.path.join(HERE, "videos")
BUILD = os.path.join(HERE, "build")

# The one voice of every video. Change it here and every line is said again.
VOICE = {"model": "kokoro-v1.0", "voice": "bf_emma", "lang": "en-gb", "speed": 1.0}


def run(cmd, **kw):
    done = subprocess.run(cmd, capture_output=True, text=True, **kw)
    if done.returncode != 0:
        raise SystemExit(f"{' '.join(map(str, cmd[:3]))} failed:\n{(done.stderr or done.stdout)[-1500:]}")
    return done.stdout


def seconds(path):
    out = run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path])
    return float(out.strip())


def load(name):
    folder = os.path.join(VIDEOS, name)
    with open(os.path.join(folder, "video.json"), encoding="utf-8") as f:
        spec = json.load(f)
    ids = [s["id"] for s in spec["steps"]]
    if len(set(ids)) != len(ids):
        raise SystemExit(f"{name}: two steps share an id")
    for n, step in enumerate(spec["steps"], 1):
        step["dir"] = f"{n:02d}-{step['id']}"
    return folder, spec


_kokoro = None


def say(text, path):
    """Synthesise one line. The model is loaded once, on the first line."""
    global _kokoro
    if _kokoro is None:
        home = os.environ.get("SITE_MEDIA_KOKORO", "")
        try:
            from kokoro_onnx import Kokoro
        except ImportError:
            raise SystemExit("the voice needs kokoro-onnx and soundfile: pip install kokoro-onnx soundfile")
        _kokoro = Kokoro(os.path.join(home, "kokoro-v1.0.onnx"), os.path.join(home, "voices-v1.0.bin"))
    import soundfile

    samples, rate = _kokoro.create(text, voice=VOICE["voice"], speed=VOICE["speed"], lang=VOICE["lang"])
    soundfile.write(path, samples, rate)


def voice(folder, spec, name):
    """Each step's voice.wav: an override if the video has one, else the
    line said by the model, kept until the line or the voice changes."""
    for step in spec["steps"]:
        out = os.path.join(BUILD, name, "steps", step["dir"])
        os.makedirs(out, exist_ok=True)
        wav, mark = os.path.join(out, "voice.wav"), os.path.join(out, "voice.key")
        override = os.path.join(folder, "overrides", step["dir"], "voice.wav")
        text = (step.get("say") or "").strip()
        key = hashlib.sha256(json.dumps([text, VOICE, os.path.getmtime(override) if os.path.exists(override) else 0]).encode()).hexdigest()
        if not (os.path.exists(wav) and os.path.exists(mark) and open(mark).read() == key):
            if os.path.exists(override):
                shutil.copy(override, wav)
            elif text:
                say(text, wav)
            else:  # a step with nothing said is half a second of quiet
                run(["ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=24000:cl=mono", "-t", "0.5", wav])
            open(mark, "w").write(key)
        step["seconds"] = round(seconds(wav), 2)


def record(spec, name, check=False):
    out = os.path.join(BUILD, name)
    os.makedirs(out, exist_ok=True)
    job = os.path.join(out, "job.json")
    with open(job, "w", encoding="utf-8") as f:
        json.dump(spec, f)
    url = os.environ.get("SITE_MEDIA_URL")
    if not url:
        raise SystemExit("set SITE_MEDIA_URL, SITE_MEDIA_EMAIL and SITE_MEDIA_PASSWORD")
    cmd = ["node", os.path.join(HERE, "record.mjs"), job, out, url] + (["--check"] if check else [])
    done = subprocess.run(cmd, capture_output=True, text=True)
    sys.stdout.write(done.stdout)
    if done.returncode != 0:
        raise SystemExit(done.stderr.strip() or "the recording failed")


def cut(folder, spec, name, only=None):
    """Each step's clip.mp4 (its picture, cut from the session, with its
    voice), then the whole video, its poster, captions and transcript."""
    base = os.path.join(BUILD, name)
    with open(os.path.join(base, "raw-timeline.json"), encoding="utf-8") as f:
        raw = {t["id"]: t for t in json.load(f)}
    session = os.path.join(base, "session.webm")
    parts, timeline, at = [], [], 0.0
    for step in spec["steps"]:
        out = os.path.join(base, "steps", step["dir"])
        clip, wav = os.path.join(out, "clip.mp4"), os.path.join(out, "voice.wav")
        picture = os.path.join(folder, "overrides", step["dir"], "clip.mp4")
        t = raw[step["id"]]
        length = max(t["end"] - t["start"], step["seconds"] + 0.3)
        if only in (None, step["id"]) or not os.path.exists(clip):
            source = ["-i", picture] if os.path.exists(picture) else ["-ss", f"{t['start']:.3f}", "-t", f"{t['end'] - t['start']:.3f}", "-i", session]
            run(
                ["ffmpeg", "-y", *source, "-i", wav, "-filter_complex",
                 # hold the last frame if the voice outlasts the picture; pad the voice with quiet to the end
                 f"[0:v]fps=30,scale={1080 if spec.get('viewport') == 'phone' else 1920}:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=30,trim=duration={length:.3f},setpts=PTS-STARTPTS[v];"
                 f"[1:a]aresample=48000,apad,atrim=duration={length:.3f}[a]",
                 "-map", "[v]", "-map", "[a]", "-c:v", "libx264", "-preset", "slow", "-crf", "26", "-pix_fmt", "yuv420p",
                 "-c:a", "aac", "-b:a", "96k", clip]
            )
        length = seconds(clip)
        parts.append(clip)
        timeline.append({"id": step["id"], "start": round(at, 2), "end": round(at + length, 2), "say": step.get("say", "")})
        at += length

    out_dir = os.environ.get("SITE_MEDIA_OUT", os.path.join(BUILD, "out"))
    os.makedirs(out_dir, exist_ok=True)
    listing = os.path.join(base, "parts.txt")
    with open(listing, "w") as f:
        f.writelines(f"file '{p}'\n" for p in parts)
    mp4 = os.path.join(out_dir, f"{name}.mp4")
    run(["ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listing, "-c", "copy", "-movflags", "+faststart", mp4])
    run(["ffmpeg", "-y", "-ss", "1", "-i", mp4, "-frames:v", "1", "-vf", "scale=1280:-2", "-q:v", "4", os.path.join(out_dir, f"{name}.jpg")])

    def stamp(s):
        return f"{int(s // 3600):02d}:{int(s % 3600 // 60):02d}:{s % 60:06.3f}"

    with open(os.path.join(out_dir, f"{name}.vtt"), "w", encoding="utf-8") as f:
        f.write("WEBVTT\n\n")
        for t in timeline:
            if t["say"]:
                f.write(f"{stamp(t['start'])} --> {stamp(t['end'])}\n{t['say']}\n\n")
    with open(os.path.join(out_dir, f"{name}.json"), "w", encoding="utf-8") as f:
        json.dump({"title": spec["title"], "description": spec.get("description", ""), "seconds": round(at, 1), "steps": timeline}, f, indent=1, ensure_ascii=False)
    with open(os.path.join(base, "timeline.json"), "w", encoding="utf-8") as f:
        json.dump(timeline, f, indent=1, ensure_ascii=False)
    shots = os.path.join(base, "shots")
    for shot in sorted(os.listdir(shots)) if os.path.isdir(shots) else []:
        if shot.endswith(".png") and not shot.startswith("FAILED-"):
            os.makedirs(os.path.join(out_dir, "screenshots"), exist_ok=True)
            src, dst = os.path.join(shots, shot), os.path.join(out_dir, "screenshots", shot[:-4])
            run(["ffmpeg", "-y", "-i", src, "-c:v", "libwebp", "-quality", "82", dst + ".webp"])
            # The copy a phone downloads instead (openv.app's /features, srcset):
            # 1280 wide, for a shot wider than that. A PNG's width is bytes 16-19.
            with open(src, "rb") as f:
                width = int.from_bytes(f.read(24)[16:20], "big")
            if width > 1280:
                run(["ffmpeg", "-y", "-i", src, "-vf", "scale=1280:-2", "-c:v", "libwebp", "-quality", "80", dst + "-1280.webp"])
    size = os.path.getsize(mp4) / 1e6
    print(f"{name}: {at:.0f} s, {size:.1f} MB, {len(parts)} steps -> {mp4}")
    if size > 50:
        raise SystemExit(f"{name}.mp4 is over 50 MB: GitHub warns there and refuses at 100")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("command", choices=["check", "build", "export", "list"])
    ap.add_argument("video", nargs="?")
    ap.add_argument("dest", nargs="?")
    ap.add_argument("--record", action="store_true", help="record the screen again even if a recording is there")
    ap.add_argument("--only", help="redo this one step's voice and clip, then join")
    args = ap.parse_args()

    if args.command == "list":
        for name in sorted(os.listdir(VIDEOS)):
            if os.path.exists(os.path.join(VIDEOS, name, "video.json")):
                _, spec = load(name)
                print(f"{name}: {spec['title']} ({len(spec['steps'])} steps)")
        return
    if not args.video:
        ap.error("name a video")
    folder, spec = load(args.video)
    if args.command == "check":
        record(spec, args.video, check=True)
        return
    if args.command == "export":
        dest = args.dest or os.path.join(BUILD, "export", args.video)
        shutil.copytree(os.path.join(BUILD, args.video, "steps"), os.path.join(dest, "steps"), dirs_exist_ok=True)
        shutil.copy(os.path.join(BUILD, args.video, "timeline.json"), dest)
        print(f"exported the step clips, voices and timeline to {dest}")
        return
    voice(folder, spec, args.video)
    if args.record or not os.path.exists(os.path.join(BUILD, args.video, "session.webm")):
        record(spec, args.video)
    cut(folder, spec, args.video, only=args.only)


if __name__ == "__main__":
    main()
