#!/usr/bin/env python3
"""Tests of scripts/changelog.py: fragments in changelog.d and their fold
into the «[Не выпущено]» section of CHANGELOG.md.

Usage: python3 scripts/changelog-test.py
"""
import importlib.util
import os
import shutil
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("changelog", os.path.join(HERE, "changelog.py"))
changelog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(changelog)

HEAD = """# Журнал изменений

Вступление.

"""
RELEASED = """## [1.2.3] — 2026-10-23

### Исправлено

- Старое.
"""


class Repo(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.root)
        os.mkdir(os.path.join(self.root, "changelog.d"))
        self.write("changelog.d/README.md", "# Фрагменты\n\nНе фрагмент.\n")

    def write(self, rel, text):
        with open(os.path.join(self.root, rel), "w", encoding="utf-8") as f:
            f.write(text)

    def read(self, rel):
        with open(os.path.join(self.root, rel), encoding="utf-8") as f:
            return f.read()

    def fragments(self):
        return sorted(n for n in os.listdir(os.path.join(self.root, "changelog.d")) if n != "README.md")


class Check(Repo):
    def test_good_fragment(self):
        self.write("changelog.d/a.md", "### Исправлено\n\n- Пункт.\n  Продолжение.\n")
        self.assertEqual(changelog.check(self.root), [])

    def test_bad_fragments(self):
        cases = {
            "text before a section": "Текст.\n\n### Исправлено\n\n- Пункт.\n",
            "unknown section": "### Прочее\n\n- Пункт.\n",
            "empty section": "### Исправлено\n\n### Добавлено\n\n- Пункт.\n",
            "version heading": "## [1.3.0]\n\n### Исправлено\n\n- Пункт.\n",
            "section twice": "### Исправлено\n\n- А.\n\n### Исправлено\n\n- Б.\n",
            "empty file": "\n",
            "paragraph, not an item": "### Исправлено\n\nТекст без дефиса.\n",
        }
        for name, text in cases.items():
            with self.subTest(name):
                self.write("changelog.d/bad.md", text)
                self.assertNotEqual(changelog.check(self.root), [], name)

    def test_only_markdown_files(self):
        self.write("changelog.d/note.txt", "x")
        self.assertNotEqual(changelog.check(self.root), [])


class Fold(Repo):
    def test_sections_merge_in_order_and_fragments_go(self):
        self.write("CHANGELOG.md", HEAD + "## [Не выпущено]\n\n### Исправлено\n\n- Было в файле.\n\n" + RELEASED)
        self.write("changelog.d/b-fix.md", "### Исправлено\n\n- Из b.\n")
        self.write("changelog.d/a-break.md", "### Добавлено\n\n- Новое из a.\n\n### Миграция\n\n- **Ломает** из a.\n  Что поменять.\n")
        self.assertEqual(changelog.fold(self.root), 2)
        self.assertEqual(self.read("CHANGELOG.md"), HEAD + """## [Не выпущено]

### Миграция

- **Ломает** из a.
  Что поменять.

### Добавлено

- Новое из a.

### Исправлено

- Было в файле.

- Из b.

""" + RELEASED)
        self.assertEqual(self.fragments(), [])

    def test_nothing_to_fold_leaves_the_file(self):
        text = HEAD + "## [Не выпущено]\n\n### Изменено\n\n- А.\n- Б.\n\n" + RELEASED
        self.write("CHANGELOG.md", text)
        self.assertEqual(changelog.fold(self.root), 0)
        self.assertEqual(self.read("CHANGELOG.md"), text)

    def test_empty_unreleased_takes_fragments(self):
        self.write("CHANGELOG.md", HEAD + "## [Не выпущено]\n\n" + RELEASED)
        self.write("changelog.d/x.md", "### Безопасность\n\n- Закрыто.\n")
        changelog.fold(self.root)
        self.assertEqual(self.read("CHANGELOG.md"), HEAD + "## [Не выпущено]\n\n### Безопасность\n\n- Закрыто.\n\n" + RELEASED)

    def test_bad_fragment_folds_nothing(self):
        text = HEAD + "## [Не выпущено]\n\n" + RELEASED
        self.write("CHANGELOG.md", text)
        self.write("changelog.d/good.md", "### Исправлено\n\n- Хорошо.\n")
        self.write("changelog.d/bad.md", "### Прочее\n\n- Плохо.\n")
        with self.assertRaises(SystemExit):
            changelog.fold(self.root)
        self.assertEqual(self.read("CHANGELOG.md"), text)
        self.assertEqual(self.fragments(), ["bad.md", "good.md"])


class TheRepository(unittest.TestCase):
    def test_fragments_and_unreleased_are_well_formed(self):
        root = os.path.dirname(HERE)
        self.assertEqual(changelog.check(root), [])


if __name__ == "__main__":
    unittest.main()
