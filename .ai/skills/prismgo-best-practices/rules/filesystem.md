# Filesystem

## Use Logical Disk Keys

Use `filesystem` facades and logical keys. Do not concatenate storage paths in services.

Incorrect:

```go
path := "storage/app/public/" + name
return os.WriteFile(path, body, 0o644)
```

Correct:

```go
func StoreExport(ctx context.Context, key string, body []byte) error {
	return filesystem.Disk("public").Put(ctx, key, body)
}
```

## Keep Disk Roots In Configuration

Disk root, URL, visibility, and driver settings belong in filesystem config.

```json
{
  "default": "local",
  "disks": {
    "public": {
      "driver": "local",
      "root": "storage/app/public",
      "url": "/storage"
    }
  }
}
```

## Stream Large Files

Use stream APIs for large objects instead of loading all bytes into memory.

```go
reader, info, err := filesystem.OpenStream(ctx, key)
if err != nil {
	return err
}
defer reader.Close()
_ = info
```

## Validate User-Provided Keys

Reject absolute paths, traversal, and control characters before passing keys to storage.

Incorrect:

```go
return filesystem.Get(ctx, c.Query("path"))
```

Correct:

```go
key := cleanStorageKey(c.Query("path"))
if key == "" {
	return nil, ErrInvalidPath
}
return filesystem.Get(ctx, key)
```
