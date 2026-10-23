The desktop build prepares `runtime.zip` here from an explicit code-only allowlist.
It contains no .env, Git history, manuscripts or private user files. The archive
is generated locally and ignored by Git. A development build without it reports
that no bundled runtime is available. See ../../README.md.
