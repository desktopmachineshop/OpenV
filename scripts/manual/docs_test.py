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


class Keys(unittest.TestCase):
    """Every context key the app sends lands on a topic, directly or through a parent."""

    def test_a_key_resolves_through_its_parents(self):
        known = {'vv', 'org.settings'}
        self.assertTrue(docs.resolves('vv.coverage', known))
        self.assertTrue(docs.resolves('org.settings.usage', known))
        self.assertFalse(docs.resolves('org.runs', known))

    def test_every_key_the_app_sends_has_a_topic(self):
        keys = docs.app_keys(docs.ROOT)
        # Each kind of site is read: a page, a tab map, a step list, a
        # dialog, a help link and a context hook.
        for key in ('org.settings', 'org.settings.runners', 'guided.step.review', 'projects.download',
                    'vv.coverage', 'links.create'):
            self.assertIn(key, keys)
        docs.check_keys(docs.ROOT)

    def test_an_unknown_key_fails(self):
        with tempfile.TemporaryDirectory() as d:
            src = Path(d, docs.FRONTEND)
            (src / 'views').mkdir(parents=True)
            (src / 'pages.ts').write_text("help: { topic: 'vv', key: 'vv' },\n")
            (src / 'views' / 'X.tsx').write_text(
                "useHelpContext(open ? 'nowhere.at.all' : null);\n"
                "<HelpLink helpKey={`vv.${x}`} label=\"x\" />\n"
                "<Modal help={{ key: 'vv.runs', fragment: 'not-a-key-site', label: 'l' }} />\n"
            )
            Path(d, docs.MANUAL).mkdir()
            Path(d, docs.MANUAL, 'help-contexts.yaml').write_text('    default: start\n    routes:\n      /a: zzz\n')
            keys = docs.app_keys(d)
            self.assertEqual(sorted(keys), ['nowhere.at.all', 'vv', 'vv.runs'])
            with self.assertRaises(docs.DocsError) as err:
                docs.check_keys(d)
            self.assertIn('nowhere.at.all', str(err.exception))
            self.assertIn('zzz', str(err.exception))


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


if __name__ == '__main__':
    unittest.main()
