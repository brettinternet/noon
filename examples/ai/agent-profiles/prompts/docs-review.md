Check whether this repository's documentation still matches its code. Do not modify files.

Read README.md and every Markdown file under docs/. For each concrete claim about commands, flags,
configuration keys, defaults, file paths, or environment variables, find the code that implements
it. Report only mismatches, as a Markdown table with columns: doc file:line, claim, what the code
actually does (file:line), suggested fix.

If everything matches, say so in one line. Do not report style issues.
