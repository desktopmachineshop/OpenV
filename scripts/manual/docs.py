#!/usr/bin/env python3
"""The user manual's rules, as CI and the release workflows apply them.

The manual is an OpenD project in manual/ (docs/docs-maintenance.md). Until
the help drawer reads it from OpenD (docs/plans/opend-help.md), the chapters
the app serves stay in frontend/src/manual/chapters, and manual/topics and
manual/maps are generated from them: edit the chapter, then run `sync`.

  sync [--check]           regenerate manual/topics, manual/maps and
                           manual/redirects.json from the chapters; --check
                           fails instead when they are out of date. Both
                           fail on a context key the app sends that no
                           topic answers (check_keys)
  check-pr --base REF      a pull request that changes what users see also
                           changes the manual, or carries no-docs-change
  line NOTES               the docs line (major.minor) of the newest release
  stamp NOTES CONFIG       set CONFIG's `release:` to that line

Stdlib-only Python 3, like every script in scripts/.
"""

import argparse
import html
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
import release_notes as rn  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent
CHAPTERS = 'frontend/src/manual/chapters'
MANUAL = 'manual'
APP = 'openv-web'
SITE = 'https://openv.app'

# Chapter order and slugs, as in frontend/src/manual/index.ts: the slugs are
# the old /manual/<slug> deep links that redirects.json maps to topics.
ORDER = [
    ('getting-started', 'gettingStarted'),
    ('projects', 'projects'),
    ('product-overview', 'productOverview'),
    ('artifacts', 'artifacts'),
    ('links', 'links'),
    ('guided-wizard', 'guidedWizard'),
    ('board', 'board'),
    ('agents', 'agents'),
    ('crews', 'crews'),
    ('runs-runners', 'runsRunners'),
    ('automations', 'automations'),
    ('interviews', 'interviews'),
    ('vv', 'vv'),
    ('org-settings', 'orgSettings'),
    ('platform-admin', 'platformAdmin'),
    ('troubleshooting', 'troubleshooting'),
]

# Context keys of the openv-web app (manual/help-contexts.yaml). A chapter's
# keys go on its overview topic, a section's on the topic its `##` heading
# becomes, found by heading id. A key falls back to its parents, so
# vv.runs opens the vv topic wherever vv.runs has none.
CHAPTER_KEYS = {
    'getting-started': ['start'],
    'projects': ['projects'],
    'product-overview': ['overview'],
    'artifacts': ['requirements'],
    'links': ['links'],
    'guided-wizard': ['guided'],
    'board': ['board'],
    'agents': ['agents'],
    'crews': ['crew'],
    'runs-runners': ['agent-runs'],
    'automations': ['automations'],
    'interviews': ['interviews'],
    'vv': ['vv'],
    # `settings` was the project settings page's key here before the page's
    # topic moved to Projects & members; it stays on the workspace settings
    # chapter's overview only through org.settings.
    'org-settings': ['org.settings'],
    'platform-admin': ['admin'],
    'troubleshooting': ['troubleshooting', 'activity'],
}
SECTION_KEYS = {
    ('artifacts', 'baselines'): ['baselines'],
    ('artifacts', 'review-status'): ['review'],
    ('artifacts', 'figures'): ['requirements.figures'],
    ('artifacts', 'history'): ['requirements.history'],
    ('links', 'the-traceability-matrix'): ['matrix'],
    ('board', 'to-dos-the-same-work-by-person'): ['todos'],
    ('vv', 'evidence-for-physical-and-manual-tests'): ['evidence'],
    ('vv', 'test-runs'): ['vv.runs'],
    ('vv', 'baselines'): ['vv.baselines'],
    ('runs-runners', 'runs-with-no-project-workspace-runs'): ['org.runs'],
    ('runs-runners', 'pending-approvals-proposal-review'): ['agent-runs.approvals'],
    ('runs-runners', 'runner-types'): ['org.settings.runners'],
    ('org-settings', 'members'): ['org.settings.members'],
    ('org-settings', 'ai-providers-admin-only'): ['org.settings.providers'],
    ('projects', 'project-access-members'): ['settings.members'],
    ('projects', 'share-links'): ['settings.share-links'],
    ('guided-wizard', 'the-vv-assistant-chat'): ['guided.assistant'],
    # In-page help (hooks/useHelp.tsx, HelpLink): tabs, steps, dialogs and
    # section headings. Each falls back to its page's key until it has one.
    ('getting-started', 'help-on-every-page'): ['help'],
    ('getting-started', 'whats-new'): ['whats-new'],
    ('projects', 'inside-a-project'): ['settings'],
    ('projects', 'creating-a-project'): ['projects.create'],
    ('projects', 'downloading-a-project'): ['projects.download'],
    ('projects', 'import'): ['projects.import'],
    ('projects', 'deleting-a-project'): ['settings.danger'],
    ('artifacts', 'quality-rules-and-findings'): ['quality', 'settings.quality', 'org.settings.quality'],
    ('links', 'creating-links'): ['links.create'],
    ('links', 'impact-analysis'): ['impact'],
    ('guided-wizard', 'starting-resuming-modifying'): ['guided.start'],
    ('guided-wizard', 'the-eight-steps'): ['guided.step'],
    ('board', 'the-card-drawer'): ['board.card'],
    ('agents', 'launching-a-run'): ['agents.launch'],
    ('agents', 'the-agent-editor'): ['agents.editor'],
    ('crews', 'connecting-nodes'): ['crew.connect'],
    ('automations', 'creating-an-automation'): ['automations.create'],
    ('runs-runners', 'connect-an-ai-app-to-a-workspace'): ['connect'],
    ('runs-runners', 'repositories'): ['settings.repos'],
    ('runs-runners', 'per-project-agent-auth'): ['settings.agents'],
    ('vv', 'coverage-rollup'): ['vv.coverage'],
    ('vv', 'gaps'): ['vv.gaps'],
    ('vv', 'the-pdf-report'): ['vv.report'],
    ('org-settings', 'general'): ['org.settings.general'],
    ('org-settings', 'limits'): ['org.settings.limits'],
    ('org-settings', 'teams'): ['org.settings.teams'],
}


class DocsError(Exception):
    pass


# --- sync: markdown chapters -> DITA ----------------------------------------


def heading_id(text):
    """Mirrors headingId() in frontend/src/manual/index.ts."""
    t = re.sub(r'[^a-z0-9\s-]', '', text.lower()).strip()
    return re.sub(r'\s+', '-', t)


def chapter_markdown(source):
    """The markdown of a chapter module: its `const content = `...`` string."""
    body = source.split('const content = `', 1)[1].rsplit('`;', 1)[0]
    return body.replace('\\`', '`').replace('\\$', '$').replace('\\\\', '\\')


INLINE = re.compile(r'`([^`]+)`|\*\*(.+?)\*\*|\*(.+?)\*|\[([^\]]+)\]\(([^)]+)\)')


def inline(text):
    out, i = [], 0
    for m in INLINE.finditer(text):
        out.append(html.escape(text[i:m.start()], quote=False))
        code, bold, ital, ltext, href = m.groups()
        if code is not None:
            out.append(f'<codeph>{html.escape(code, quote=False)}</codeph>')
        elif bold is not None:
            out.append(f'<b>{inline(bold)}</b>')
        elif ital is not None:
            out.append(f'<i>{inline(ital)}</i>')
        else:
            url = SITE + href if href.startswith('/') else href
            out.append(f'<xref href="{html.escape(url)}" scope="external" format="html">{inline(ltext)}</xref>')
        i = m.end()
    out.append(html.escape(text[i:], quote=False))
    return ''.join(out)


LIST_ITEM = re.compile(r'^(\s*)([-*]|\d+\.)\s+(.*)$')


def blocks(lines, indent='    '):
    """Markdown block lines -> DITA body elements, one per line."""
    out, i = [], 0
    while i < len(lines):
        line = lines[i]
        if not line.strip():
            i += 1
        elif line.startswith('### '):
            j = i + 1
            while j < len(lines) and not lines[j].startswith('### '):
                j += 1
            out.append(f'{indent}<section id="{heading_id(line[4:])}">')
            out.append(f'{indent}  <title>{inline(line[4:].strip())}</title>')
            out.extend(blocks(lines[i + 1:j], indent + '  '))
            out.append(f'{indent}</section>')
            i = j
        elif line.startswith('|'):
            rows = []
            while i < len(lines) and lines[i].startswith('|'):
                cells = [c.strip() for c in lines[i].strip().strip('|').split('|')]
                if not all(re.fullmatch(r':?-{3,}:?', c) for c in cells):
                    rows.append(cells)
                i += 1
            out.append(f'{indent}<simpletable>')
            for n, row in enumerate(rows):
                tag = 'sthead' if n == 0 else 'strow'
                out.append(f'{indent}  <{tag}>' + ''.join(f'<stentry>{inline(c)}</stentry>' for c in row) + f'</{tag}>')
            out.append(f'{indent}</simpletable>')
        elif line.startswith('>'):
            text = []
            while i < len(lines) and lines[i].startswith('>'):
                text.append(lines[i][1:].strip())
                i += 1
            out.append(f'{indent}<note>{inline(" ".join(text))}</note>')
        elif LIST_ITEM.match(line):
            j = i
            items = []
            while j < len(lines) and (
                LIST_ITEM.match(lines[j])
                or (lines[j].startswith(' ') and lines[j].strip())
                or (not lines[j].strip() and j + 1 < len(lines) and lines[j + 1].startswith(' '))
            ):
                items.append(lines[j])
                j += 1
            out.extend(list_xml(items, indent))
            i = j
        else:
            para = []
            while (
                i < len(lines)
                and lines[i].strip()
                and not lines[i].startswith(('|', '>', '### '))
                and not LIST_ITEM.match(lines[i])
            ):
                para.append(lines[i].strip())
                i += 1
            out.append(f'{indent}<p>{inline(" ".join(para))}</p>')
    return out


def list_xml(lines, indent):
    first = LIST_ITEM.match(lines[0])
    base = len(first.group(1))
    tag = 'ol' if first.group(2)[0].isdigit() else 'ul'
    items = []  # [first line, [following lines]]
    for line in lines:
        m = LIST_ITEM.match(line)
        if m and len(m.group(1)) == base:
            items.append([m.group(3).strip(), []])
        else:
            items[-1][1].append(line)
    out = [f'{indent}<{tag}>']
    for text, rest in items:
        words, child = [text], []
        for k, line in enumerate(rest):
            m = LIST_ITEM.match(line)
            if m and len(m.group(1)) > base:
                child = [c for c in rest[k:] if c.strip()]
                break
            if line.strip():
                words.append(line.strip())
        if child:
            out.append(f'{indent}  <li>{inline(" ".join(words))}')
            out.extend(list_xml(child, indent + '    '))
            out.append(f'{indent}  </li>')
        else:
            out.append(f'{indent}  <li>{inline(" ".join(words))}</li>')
    out.append(f'{indent}</{tag}>')
    return out


def topic(tid, title, lines, keys):
    """A concept topic. Its first paragraph is the short description when it
    is short and plain prose: the help drawer and search show it."""
    paras = '\n'.join(lines).strip().split('\n\n', 1)
    first = paras[0].replace('\n', ' ').strip()
    short, rest = None, lines
    if first and not first.startswith(('|', '>', '#', '-', '*')) and not re.match(r'\d+\. ', first) and len(first) <= 320:
        short = first
        rest = (paras[1] if len(paras) > 1 else '').split('\n')
    xml = [
        '<?xml version="1.0" encoding="UTF-8"?>',
        '<!DOCTYPE concept PUBLIC "-//OASIS//DTD DITA Concept//EN" "concept.dtd">',
        f'<concept id="{tid}">',
        f'  <title>{inline(title)}</title>',
    ]
    if short:
        xml.append(f'  <shortdesc>{inline(short)}</shortdesc>')
    if keys:
        xml.append('  <prolog>')
        xml.extend(f'    <resourceid appname="{APP}" appid="{k}"/>' for k in keys)
        xml.append('  </prolog>')
    body = blocks(rest)
    if body:
        xml += ['  <conbody>', *body, '  </conbody>']
    else:
        xml.append('  <conbody/>')
    xml.append('</concept>')
    return '\n'.join(xml) + '\n'


def generate(chapters_dir):
    """{relative path under manual/: content} for every generated file."""
    files, links, refs, seen = {}, {}, [], set()
    for slug, name in ORDER:
        md = chapter_markdown((chapters_dir / f'{name}.ts').read_text()).strip('\n').split('\n')
        if not md[0].startswith('# '):
            raise DocsError(f'{name}.ts: a chapter starts with its "# " title')
        parts = [['', []]]
        for line in md[1:]:
            if line.startswith('## '):
                parts.append([line[3:].strip(), []])
            else:
                parts[-1][1].append(line)
        files[f'topics/{slug}/index.dita'] = topic(slug, md[0][2:].strip(), parts[0][1], CHAPTER_KEYS.get(slug, []))
        links[f'/manual/{slug}'] = f'topics/{slug}/index'
        refs.append(f'  <topicref href="../topics/{slug}/index.dita">')
        for heading, lines in parts[1:]:
            hid = heading_id(heading)
            if (slug, hid) in seen:
                raise DocsError(f'{name}.ts: two sections are headed "{heading}"')
            seen.add((slug, hid))
            files[f'topics/{slug}/{hid}.dita'] = topic(hid, heading, lines, SECTION_KEYS.get((slug, hid), []))
            links[f'/manual/{slug}#{hid}'] = f'topics/{slug}/{hid}'
            refs.append(f'    <topicref href="../topics/{slug}/{hid}.dita"/>')
        refs.append('  </topicref>')
    stale = sorted(f'{s}#{h}' for s, h in SECTION_KEYS if (s, h) not in seen)
    if stale:
        raise DocsError('SECTION_KEYS names sections no chapter has: ' + ', '.join(stale))
    files['maps/manual.ditamap'] = (
        '<?xml version="1.0" encoding="UTF-8"?>\n'
        '<!DOCTYPE map PUBLIC "-//OASIS//DTD DITA Map//EN" "map.dtd">\n'
        '<!-- Generated by scripts/manual/docs.py sync from frontend/src/manual/chapters. -->\n'
        '<map>\n  <title>OpenV User Manual</title>\n\n' + '\n'.join(refs) + '\n</map>\n'
    )
    files['redirects.json'] = json.dumps(links, indent=2) + '\n'
    return files


# --- keys: every context key the app sends has a topic ------------------------

FRONTEND = 'frontend/src'
KEY = r'[a-z][a-z0-9-]*(?:\.[a-z0-9-]+)*'
# Where the app names a key: pages.ts's `key:`, useHelpContext(...), a
# HelpLink's helpKey=, a Modal's help={{ key: ... }}, the wizard's helpKey,
# and a *_HELP_KEYS list or map (hooks/useHelp.tsx). A template literal
# stands for its static prefix (`guided.${x}` must resolve as guided).
LITERAL = re.compile(r"['\"](" + KEY + r")['\"]|`(" + KEY + r"\.?)\$\{")
# In a map, the values: a quoted property name is not a key.
VALUE = re.compile(r"['\"](" + KEY + r")['\"](?!\s*:)")
KEY_FIELD = re.compile(r"\bkey: ['\"](" + KEY + r")['\"]")
KEY_SITES = [
    (re.compile(r'useHelpContext\((.*?)\);', re.S), LITERAL),
    (re.compile(r'helpKey=(\"[^\"]*\"|\{[^}]*\})'), LITERAL),
    (re.compile(r'help=\{\{(.*?)\}\}', re.S), KEY_FIELD),
    (re.compile(r'const helpKey = (.*?);', re.S), LITERAL),
    (re.compile(r'[A-Z_]*HELP_KEYS\b[^=\n]*= [\[{](.*?)[\]}];', re.S), VALUE),
]


def app_keys(root):
    """Each context key the app sends, with the files that send it."""
    found = {}
    src = Path(root) / FRONTEND
    pages = src / 'pages.ts'
    for m in re.finditer(r"\bkey: '(" + KEY + r")'", pages.read_text()):
        found.setdefault(m.group(1), set()).add('frontend/src/pages.ts')
    for f in sorted(src.rglob('*.ts*')):
        if '.test.' in f.name or f.suffix not in ('.ts', '.tsx') or f == pages:
            continue
        text = f.read_text()
        if 'elp' not in text:
            continue
        for site, literal in KEY_SITES:
            for m in site.finditer(text):
                for lit in literal.finditer(m.group(1)):
                    key = next(g for g in lit.groups() if g).rstrip('.')
                    found.setdefault(key, set()).add(f.relative_to(root).as_posix())
    return found


def route_keys(root):
    """The keys manual/help-contexts.yaml's routes and default name."""
    text = (Path(root) / MANUAL / 'help-contexts.yaml').read_text()
    keys = re.findall(r'^\s+/\S*: (' + KEY + r')\s*$', text, re.M)
    keys += re.findall(r'^\s+default: (' + KEY + r')\s*$', text, re.M)
    return keys


def topic_keys():
    """Every key a topic carries."""
    keys = {k for ks in CHAPTER_KEYS.values() for k in ks}
    return keys | {k for ks in SECTION_KEYS.values() for k in ks}


def resolves(key, known):
    """Whether key, or a parent of it (vv.runs -> vv), is a topic's key."""
    parts = key.split('.')
    return any('.'.join(parts[:i]) in known for i in range(len(parts), 0, -1))


def check_keys(root):
    """Fails on a key the app or help-contexts.yaml sends that no topic answers."""
    known = topic_keys()
    missing = sorted(
        f'{key} ({", ".join(sorted(where))})' for key, where in app_keys(root).items() if not resolves(key, known)
    )
    missing += sorted(f'{k} (manual/help-contexts.yaml)' for k in route_keys(root) if not resolves(k, known))
    if missing:
        raise DocsError(
            'context keys with no topic, directly or through a parent: ' + '; '.join(missing)
            + '. Give a section the key in SECTION_KEYS (scripts/manual/docs.py) and sync'
        )


GENERATED = ('topics', 'maps', 'redirects.json')


def existing(manual):
    found = {}
    for top in GENERATED:
        p = manual / top
        if p.is_file():
            found[top] = p.read_text()
        elif p.is_dir():
            for f in sorted(p.rglob('*')):
                if f.is_file():
                    found[f.relative_to(manual).as_posix()] = f.read_text()
    return found


def cmd_sync(args):
    root = Path(args.root)
    manual = root / MANUAL
    want = generate(root / CHAPTERS)
    check_keys(root)
    have = existing(manual)
    if args.check:
        drift = sorted(p for p in set(want) | set(have) if want.get(p) != have.get(p))
        if drift:
            raise DocsError(
                'manual/ is out of date with frontend/src/manual/chapters: '
                + ', '.join(drift[:5]) + (' …' if len(drift) > 5 else '')
                + '. Run: python3 scripts/manual/docs.py sync'
            )
        print(f'manual/ matches the chapters ({len(want)} files)')
        return
    for top in GENERATED:
        p = manual / top
        if p.is_dir():
            shutil.rmtree(p)
        elif p.exists():
            p.unlink()
    for rel, content in want.items():
        (manual / rel).parent.mkdir(parents=True, exist_ok=True)
        (manual / rel).write_text(content)
    print(f'wrote {len(want)} files under manual/')


# --- check-pr: the docs travel with the code --------------------------------

# Paths whose change means users see something different: a page added,
# moved or retitled, or a release feature registered.
USER_FACING = ('frontend/src/pages.ts', 'internal/domain/release/features.go')
DOCS = ('frontend/src/manual/', 'manual/')


def docs_required(changed, base_notes, head_notes):
    """The reasons this change needs a manual update, or [] when it has one
    or needs none."""
    if any(p.startswith(DOCS) for p in changed):
        return []
    reasons = [f'{p} changed' for p in USER_FACING if p in changed]
    base_unreleased = rn.parse(base_notes, strict=False)[0] if base_notes.strip() else []
    head_unreleased = rn.parse(head_notes, strict=False)[0] if head_notes.strip() else []
    reasons += [
        f'new feature in RELEASE_NOTES.md: {bullet}'
        for group, bullet in head_unreleased
        if group == rn.FEATURES and (group, bullet) not in base_unreleased
    ]
    return reasons


def git(*args, root):
    return subprocess.run(['git', *args], cwd=root, check=True, capture_output=True, text=True).stdout


def cmd_check_pr(args):
    root = Path(args.root)
    changed = [p for p in git('diff', '--name-only', args.base, 'HEAD', root=root).split('\n') if p]
    try:
        base_notes = git('show', f'{args.base}:RELEASE_NOTES.md', root=root)
    except subprocess.CalledProcessError:
        base_notes = ''
    reasons = docs_required(changed, base_notes, (root / 'RELEASE_NOTES.md').read_text())
    if reasons:
        raise DocsError(
            'this pull request changes what users see but not the user manual:\n  '
            + '\n  '.join(reasons)
            + '\nUpdate the manual (docs/docs-maintenance.md), or label the pull request '
            "'no-docs-change' if the manual already says the right thing."
        )
    print('the manual is updated, or nothing users see changed')


# --- line and stamp: one docs line per minor release ------------------------


def docs_line(notes_text):
    version = rn.current_version(notes_text)
    if not version or not rn.VERSION.match(version):
        raise DocsError('RELEASE_NOTES.md names no released version')
    major, minor, _ = version.split('.')
    return f'{major}.{minor}'


RELEASE_LINE = re.compile(r"^release: .*$", re.M)


def stamp(config_text, line):
    if not RELEASE_LINE.search(config_text):
        raise DocsError('opend.config.yaml has no `release:` line to stamp')
    return RELEASE_LINE.sub(f"release: '{line}'", config_text, count=1)


def cmd_line(args):
    print(docs_line(Path(args.notes).read_text()))


def cmd_stamp(args):
    config = Path(args.config)
    line = docs_line(Path(args.notes).read_text())
    config.write_text(stamp(config.read_text(), line))
    print(f'{config}: release {line}')


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('--root', default=str(ROOT), help=argparse.SUPPRESS)
    sub = p.add_subparsers(dest='cmd', required=True)
    s = sub.add_parser('sync')
    s.add_argument('--check', action='store_true')
    s.set_defaults(func=cmd_sync)
    s = sub.add_parser('check-pr')
    s.add_argument('--base', required=True)
    s.set_defaults(func=cmd_check_pr)
    s = sub.add_parser('line')
    s.add_argument('notes')
    s.set_defaults(func=cmd_line)
    s = sub.add_parser('stamp')
    s.add_argument('notes')
    s.add_argument('config')
    s.set_defaults(func=cmd_stamp)
    args = p.parse_args(argv)
    try:
        args.func(args)
    except (DocsError, rn.NotesError) as e:
        print(f'docs: {e}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
