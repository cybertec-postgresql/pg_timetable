# Contributing

## Documentation

`docs/` follows the [Diátaxis](https://diataxis.fr) framework: each page is exactly one of
**Tutorial** (learning a skill by doing), **How-to Guide** (achieving a specific goal),
**Reference** (looking up a fact), or **Explanation** (understanding why). Don't mix modes
within a page — link to the appropriate quadrant instead of re-explaining or re-instructing.

New documentation files use a quadrant-prefixed name:

- `tutorial-<topic>.md`
- `how-to-<topic>.md`
- `reference-<topic>.md`
- `explanation-<topic>.md`

Existing files predating this convention (`api.md`, `database_schema.md`, `installation.md`,
`migration.md`, `samples.md`, `secret_store.md`, `yaml-format.md`) are not renamed
retroactively — renaming breaks external links and `git blame`. New pages should follow the
convention above regardless of the older filenames already in the tree.

Add every new page to the correct section of `mkdocs.yml`'s `nav`.
