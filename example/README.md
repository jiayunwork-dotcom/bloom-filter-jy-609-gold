# bloom-filter examples

Offline usage examples (no network required).

Build first:

```bash
export GOTOOLCHAIN=local CGO_ENABLED=0
go build -o /tmp/bloom-filter .
```

Build a filter and add items:

```bash
/tmp/bloom-filter add -f /tmp/filter.bin -items apple banana cherry
/tmp/bloom-filter add -f /tmp/filter.bin -items date
```

Check membership:

```bash
/tmp/bloom-filter check -f /tmp/filter.bin -item apple   # present
/tmp/bloom-filter check -f /tmp/filter.bin -item grape   # absent (definitely)
```

`add` updates the serialized filter in place; `check` never panics and exits
non-zero on bad input.
