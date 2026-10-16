# Hello, Noon

**Level:** beginner · **Shows:** argv vs shell jobs, the job environment, a cron schedule, `missed: skip`

```sh
cd examples/basics/hello
noon list                 # jobs and next run times; executes nothing
noon run hello            # foreground run; output streams to your terminal
noon run context          # NOON_PROJECT, NOON_JOB, NOON_RUN_ID, NOON_TRIGGER, ...
noon history              # recorded runs
noon logs context         # stored output of the latest context run
```

Manual jobs (no `schedule`) only run when you ask. To let `stretch` fire on its own:

```sh
noon enable               # approve this checkout's shared jobs; starts nothing
noon serve                # foreground scheduler (or: noon service install)
```

`run: [echo, hello from noon]` passes two exact arguments. `run: env | grep ...` is a
string, so it runs through `/bin/sh -c`. Prefer argv unless you need shell syntax.
