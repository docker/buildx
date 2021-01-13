# buildx stop

```
Usage:  docker buildx stop [NAME]

Stop builder instance

Options:
      --builder string   Override the configured builder instance
```

## Description

Stops the specified or current builder. This will not prevent buildx build to
restart the builder. The implementation of stop depends on the driver.
