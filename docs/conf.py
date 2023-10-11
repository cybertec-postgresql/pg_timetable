# Configuration file for the Sphinx documentation builder.
#
# This file only contains a selection of the most common options. For a full
# list see the documentation:
# https://www.sphinx-doc.org/en/master/usage/configuration.html

from __future__ import division, print_function, unicode_literals

from datetime import datetime

extensions = []
templates_path = ['templates', '_templates', '.templates']
source_suffix = ['.rst', '.md']
project = u'pg_timetable'
copyright = str(datetime.now().year)

# -- Options for EPUB output
epub_show_urls = "footnote"

exclude_patterns = ['_build']
pygments_style = 'sphinx'
htmlhelp_basename = 'pg-timetable'
html_theme = 'sphinx_rtd_theme'
file_insertion_enabled = False
latex_documents = [
  ('index', 'pg-timetable.tex', u'pg_timetable Documentation',
   u'', 'manual'),
]

