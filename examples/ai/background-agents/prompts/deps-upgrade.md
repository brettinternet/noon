Upgrade this repository's dependencies to their latest compatible versions.

1. Detect the package managers in use (go.mod, package.json/bun.lock, Cargo.toml, ...).
2. Upgrade direct dependencies within their current major versions.
3. Build and run the tests. Fix any breakage the upgrades caused, keeping changes minimal.
4. If an upgrade needs a non-trivial migration, revert just that upgrade and list it at the end
   with the reason.

Do not change unrelated code, formatting, or configuration. Finish with a short summary of what
was upgraded, what was fixed, and what was skipped.
