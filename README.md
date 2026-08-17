# bloom-filter

Space-efficient probabilistic set membership CLI with append-only persistent
storage and crash recovery. Supports both snapshot-based file format (v1/v2 with
CRC32 integrity) and an append-only store format with checkpoint/replay semantics.

## Build

```sh
go build -o bloom-filter .
```

## Usage

### Snapshot mode (single-file filter)

```sh
# Add items (creates file if not exists, default m=65536 k=7)
bloom-filter add -f data.bf -items apple,banana,cherry

# Check membership
bloom-filter check -f data.bf -item apple    # prints "present"
bloom-filter check -f data.bf -item grape    # prints "absent"

# Show filter metadata
bloom-filter info -f data.bf
```

### Append-only store mode (crash-safe, incremental)

```sh
# Add items to store (creates if not exists)
bloom-filter store-add -f data.blm -items alpha,beta,gamma

# Check membership via store
bloom-filter store-check -f data.blm -item alpha

# Write a checkpoint (full snapshot for faster future replay)
bloom-filter checkpoint -f data.blm

# Add more items after checkpoint
bloom-filter store-add -f data.blm -items delta
```

## File Formats

### Snapshot format (v2, produced by `add` command)

```
[4] magic 0x424C4D02
[1] version (2)
[4] m (uint32 big-endian)
[4] k (uint32 big-endian)
[N] bits
[4] CRC32-IEEE (over header+bits)
```

Legacy v1 format (magic 0x424C4D01, no CRC) is read-compatible.

### Store format (produced by `store-add` command)

```
[FileHeader: 4 magic + 4 filterM]
[Record]*: 1 type + 4 length + N payload + 4 CRC32
```

Record types: Add (0x01) and Checkpoint (0x02). On open, replay starts from
the last valid checkpoint. Truncated/corrupt trailing records are discarded
(crash recovery).

## Flags

| command | flag | meaning | default |
|---------|------|---------|---------|
| add | `-f` | filter file path | required |
| add | `-items` | items to add (comma/space separated) | — |
| check | `-f` | filter file path | required |
| check | `-item` | item to check | required |
| info | `-f` | filter file path | required |
| store-add | `-f` | store file path | required |
| store-add | `-m` | filter bits (new store only) | 65536 |
| store-add | `-k` | hash functions (new store only) | 7 |
| store-add | `-items` | items to add | — |
| store-check | `-f` | store file path | required |
| store-check | `-item` | item to check | required |
| checkpoint | `-f` | store file path | required |

## Package Structure

- `internal/hash` — Kirsch-Mitzenmacher double hashing (FNV-1a + FNV-1).
- `internal/filter` — BloomFilter core: Add, Test, FalsePositiveRate,
  capacity planning (OptimalK, RequiredBits, NewFromCapacity, MaxInsertions).
- `internal/codec` — Binary serialization with versioning (v1 legacy, v2 with
  CRC32 integrity check) and backward compatibility.
- `internal/store` — Append-only file storage with checkpoint/replay and
  crash recovery (truncated record discard).

## Testing

```sh
go test ./...
```

## License

MIT
