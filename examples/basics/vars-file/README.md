# Config-time variables

`vars.env` supplies only the allowlisted fields in this directory's `noon.yaml`. It is separate
from job `env_file`, which is read when a run starts. These sample values are not secrets; never
put secrets in a `vars_file`.

```sh
noon -C examples/basics/vars-file config check
noon -C examples/basics/vars-file list
```
