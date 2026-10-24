"""Tests for scripts/manual/docs.py: python3 -m unittest scripts/manual/docs_test.py"""

import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.dirname(__file__))
import docs  # noqa: E402

NOTES = """# Notes

## Unreleased

### New features

- Pending feature

## 0.23.0 — 2026-10-23

### Bug fixes

- Fixed
"""


class Convert(unittest.TestCase):
    def test_inline_marks_escape_and_link_to_the_site(self):
        self.assertEqual(
            docs.inline('**Bold** *it* `a<b` & [Pricing](/pricing)'),
            '<b>Bold</b> <i>it</i> <codeph>a&lt;b</codeph> &amp; '
            '<xref href="https://openv.app/pricing" scope="external" format="html">Pricing</xref>',
        )

    def test_heading_id_mirrors_the_manual_view(self):
        self.assertEqual(docs.heading_id('To-dos: the same work, by person'), 'to-dos-the-same-work-by-person')

    def test_blocks(self):
        md = [
            'Intro line',
            'continued.',
            '',
            '1. First',
            '   - nested **a**',
            '   - nested b',
            '2. Second',
            '',
            '| A | B |',
            '| --- | --- |',
            '| 1 | 2 |',
            '',
            '> Quoted',
            '> on.',
            '',
            '### Sub head',
            '',
            'In section.',
        ]
        self.assertEqual(
            docs.blocks(md, ''),
            [
                '<p>Intro line continued.</p>',
                '<ol>',
                '  <li>First',
                '    <ul>',
                '      <li>nested <b>a</b></li>',
                '      <li>nested b</li>',
                '    </ul>',
                '  </li>',
                '  <li>Second</li>',
                '</ol>',
                '<simpletable>',
                '  <sthead><stentry>A</stentry><stentry>B</stentry></sthead>',
                '  <strow><stentry>1</stentry><stentry>2</stentry></strow>',
                '</simpletable>',
                '<note>Quoted on.</note>',
                '<section id="sub-head">',
                '  <title>Sub head</title>',
                '  <p>In section.</p>',
                '</section>',
            ],
        )

    def test_topic_takes_a_short_first_paragraph_as_shortdesc_and_carries_keys(self):
        xml = docs.topic('t', 'Title', ['Short.', '', 'Body.'], ['vv.runs'])
        self.assertIn('<shortdesc>Short.</shortdesc>', xml)
        self.assertIn('<resourceid appname="openv-web" appid="vv.runs"/>', xml)
        self.assertIn('<p>Body.</p>', xml)
        self.assertNotIn('<p>Short.</p>', xml)

    def test_chapter_markdown_unescapes_the_template_literal(self):
        src = 'const content = `\n# T\n\nUse \\`x\\` and \\${y}.\n`;\n\nexport default content;\n'
        self.assertEqual(docs.chapter_markdown(src), '\n# T\n\nUse `x` and ${y}.\n')

    def test_the_real_chapters_generate_every_keyed_section(self):
        files = docs.generate(docs.ROOT / docs.CHAPTERS)
        self.assertIn('topics/vv/test-runs.dita', files)
        self.assertIn('appid="evidence"', files['topics/vv/evidence-for-physical-and-manual-tests.dita'])


class CheckPr(unittest.TestCase):
    base = NOTES.replace('- Pending feature\n', '- Older feature\n')

    def test_a_new_feature_needs_the_manual(self):
        reasons = docs.docs_required(['RELEASE_NOTES.md'], self.base, NOTES)
        self.assertEqual(reasons, ['new feature in RELEASE_NOTES.md: Pending feature'])

    def test_pages_and_features_need_the_manual(self):
        reasons = docs.docs_required(['frontend/src/pages.ts', 'internal/domain/release/features.go'], NOTES, NOTES)
        self.assertEqual(len(reasons), 2)

    def test_a_manual_change_satisfies_it(self):
        for path in ('manual/topics/vv/index.dita', 'frontend/src/manual/chapters/vv.ts'):
            self.assertEqual(docs.docs_required(['frontend/src/pages.ts', path], self.base, NOTES), [])

    def test_fixes_need_nothing(self):
        head = NOTES.replace('## 0.23.0', '### Bug fixes\n\n- A fix\n\n## 0.23.0')
        self.assertEqual(docs.docs_required(['RELEASE_NOTES.md'], NOTES, head), [])


class Stamp(unittest.TestCase):
    def test_line_is_major_minor_of_the_newest_release(self):
        self.assertEqual(docs.docs_line(NOTES), '0.23')

    def test_stamp_rewrites_only_the_release_line(self):
        cfg = "title: T\n# release: is stamped\nrelease: '0.22'\nmap: m\n"
        self.assertEqual(docs.stamp(cfg, '0.23'), "title: T\n# release: is stamped\nrelease: '0.23'\nmap: m\n")

    def test_stamp_needs_a_release_line(self):
        with self.assertRaises(docs.DocsError):
            docs.stamp('title: T\n', '0.23')

    def test_stamp_command_writes_the_config(self):
        with tempfile.TemporaryDirectory() as d:
            notes, cfg = Path(d, 'n.md'), Path(d, 'c.yaml')
            notes.write_text(NOTES)
            cfg.write_text("release: '0.1'\n")
            self.assertEqual(docs.main(['stamp', str(notes), str(cfg)]), 0)
            self.assertEqual(cfg.read_text(), "release: '0.23'\n")


class FakeSite(docs.Site):
    """The admin API of a docs site whose lines go live one call at a time."""

    def __init__(self, live_after, granularity='major'):
        super().__init__('https://docs.example', 't')
        self.live = ['0.21', '0.22']
        self.live_after = live_after
        self.granularity = granularity
        self.calls = []

    def call(self, method, path, body=None):
        self.calls.append((method, path, body))
        if path == '/repos':
            return [{'id': 1, 'fullName': 'other/docs'}, {'id': 7, 'fullName': 'desktopmachineshop/OpenV', 'releaseLines': self.granularity}]
        if path == '/repos/7/releases':
            self.live_after -= 1
            if self.live_after <= 0 and '0.23' not in self.live:
                self.live.append('0.23')
            return [{'releaseLine': l} for l in self.live]
        return {}


class PointSite(unittest.TestCase):
    def test_waits_for_the_line_then_sets_default_and_picker(self):
        site = FakeSite(live_after=3)
        lines = docs.point_site(site, '0.23', wait=100, every=10, sleep=lambda s: None)
        self.assertEqual(lines, ['0.23', '0.22', '0.21'])
        writes = [(m, p, b) for m, p, b in site.calls if m == 'PUT']
        self.assertEqual(writes[0], ('PUT', '/repos/7/release-lines', {'releaseLines': 'minor'}))
        self.assertEqual(writes[1][1], '/repos/7/access')
        self.assertEqual(writes[1][2]['publicEntitlements']['version'], '0.23')
        self.assertEqual(writes[2], ('PUT', '/repos/7/versions', {'mode': 'list', 'versions': ['0.23', '0.22', '0.21']}))

    def test_minor_lines_are_not_set_again(self):
        site = FakeSite(live_after=0, granularity='minor')
        docs.point_site(site, '0.23', sleep=lambda s: None)
        self.assertNotIn('/repos/7/release-lines', [p for _, p, _ in site.calls])

    def test_versions_sort_as_numbers(self):
        site = FakeSite(live_after=0, granularity='minor')
        site.live = ['0.3', '0.23', '0.22']
        self.assertEqual(docs.point_site(site, '0.23', sleep=lambda s: None), ['0.23', '0.22', '0.3'])

    def test_a_line_that_never_goes_live_fails(self):
        site = FakeSite(live_after=99)
        with self.assertRaises(docs.DocsError):
            docs.point_site(site, '0.23', wait=30, every=10, sleep=lambda s: None)
        self.assertFalse([p for m, p, _ in site.calls if m == 'PUT' and p != '/repos/7/release-lines'])

    def test_without_credentials_it_only_warns(self):
        with tempfile.TemporaryDirectory() as d:
            notes = Path(d, 'n.md')
            notes.write_text(NOTES)
            env = {k: os.environ.pop(k) for k in ('OPEND_DOCS_URL', 'OPEND_DOCS_ADMIN_TOKEN') if k in os.environ}
            try:
                self.assertEqual(docs.main(['point-site', str(notes)]), 0)
            finally:
                os.environ.update(env)


if __name__ == '__main__':
    unittest.main()
