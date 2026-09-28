import tempfile
import unittest
from pathlib import Path

from check_docs import check_file


class DocumentationChecks(unittest.TestCase):
    def test_reports_missing_relative_link(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            page = root / "guide.md"
            page.write_text("[missing](other.md)\n")
            errors = check_file(page, root, check_format=True)
            self.assertTrue(any("other.md" in error for error in errors))

    def test_accepts_existing_link_and_anchor(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "other.md").write_text("# Heading\n")
            page = root / "guide.md"
            page.write_text("[target](other.md#heading)\n")
            self.assertEqual(check_file(page, root, check_format=True), [])

    def test_reports_shell_syntax_error(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            page = root / "guide.md"
            page.write_text("```sh\nif true; then\n```\n")
            errors = check_file(page, root, check_format=True)
            self.assertTrue(any("shell example" in error for error in errors))

    def test_reports_old_command_even_with_placeholder(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            guide = root / "docs" / "guides"
            guide.mkdir(parents=True)
            page = guide / "guide.md"
            page.write_text("```sh\nwso2 product install <name>\n```\n")
            errors = check_file(page, root, check_format=True)
            self.assertTrue(any("released command name ws" in error for error in errors))

    def test_reports_formatting_in_changed_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            page = root / "guide.md"
            page.write_text("# Heading \n")
            self.assertTrue(any("trailing whitespace" in error for error in check_file(page, root, True)))
            self.assertEqual(check_file(page, root, False), [])


if __name__ == "__main__":
    unittest.main()
