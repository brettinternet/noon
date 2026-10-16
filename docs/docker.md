# Docker

The bootstrap image builds a static Go binary and runs as UID 10001. It includes
CA roots and a shell; IANA timezone data is embedded in the binary. No listener,
scheduler, or agent starts automatically.

```sh
task docker:build
docker run --rm --read-only bump:dev --json config check
docker run --rm --read-only \
  --mount type=bind,src="$(pwd)",dst=/workspace,readonly \
  bump:dev config
```

The image sets `BUMP_STATE_DIR=/state`; bootstrap inspection does not write there.
Init rejects this invocation override (rather than ignoring it). Initialize a repo
on the host, or explicitly unset the variable inside the container if you really
intend to create config there. Do not run init against a read-only mount.

Scheduling is not implemented. When delivered, the container will run
`bump serve` in the foreground. Persistent state needs a volume writable by the
container UID; repository access should be read-only unless a job explicitly needs
to modify it. Multiple owners of the same project/state must be refused.

This image intentionally contains no agent CLI, Git toolchain, Docker socket,
or host credentials. Extend the image for job tools; bind only required inputs.
An agent in a container is not automatically safe to run untrusted code.

`task docker:smoke` builds the image and exercises version/config commands with a
read-only root filesystem. Scheduler signals/recovery/child reaping will be tested
in the production-container backlog item, not claimed by this bootstrap.
