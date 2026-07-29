# adbc-driver-gizmosql

Python [ADBC](https://arrow.apache.org/adbc/) driver for
[GizmoSQL](https://gizmodata.com/gizmosql) with OAuth/SSO support.

Version 2.0 is a thin Python binding over the native Go GizmoSQL ADBC
driver (`libadbc_driver_gizmosql`, bundled in the wheel), keeping the
1.x API byte-compatible. GizmoSQL-specific behavior — the `gizmosql://`
URI scheme, DDL/DML immediate execution under GizmoSQL's lazy-execution
model, and `RETURNING` eager materialization — lives inside the shared
library and is identical across every ADBC language.

```python
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect("gizmosql://localhost:31337",
                      username="gizmosql_user",
                      password="gizmosql_password",
                      tls_skip_verify=True) as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT 1 AS value")
        print(cur.fetch_arrow_table())
```

See the [project README](https://github.com/gizmodata/gizmosql-adbc#readme)
for full documentation, including OAuth/SSO, connection profiles, and
observability.
