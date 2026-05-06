# Changelog
## [v0.1.8](https://github.com/itchyny/timefmt-go/compare/v0.1.7..v0.1.8) (2026-04-01)
* fix parsing negative year and Unix time (`%Y`, `%G`, `%s`)
* fix formatting negative year, century, Unix time (`%Y`, `%G`, `%C`, `%y`, `%g`, `%s`)
* fix `%g` parsing to use the same two-digit year threshold 69 as `%y`
* fix `%s` formatting and parsing on 32-bit platforms
* support parsing time zone offset with `%:::z`
* improve performance of parsing/formatting compound directives

## [v0.1.7](https://github.com/itchyny/timefmt-go/compare/v0.1.6..v0.1.7) (2025-10-01)
* refactor code using built-in `min` and `max` functions

## [v0.1.6](https://github.com/itchyny/timefmt-go/compare/v0.1.5..v0.1.6) (2024-06-01)
* support parsing week directives (`%A`, `%a`, `%w`, `%u`, `%V`, `%U`, `%W`)
* validate range of values on parsing directives
* fix formatting `%l` to show `12` at midnight

## [v0.1.5](https://github.com/itchyny/timefmt-go/compare/v0.1.4..v0.1.5) (2022-12-01)
* support parsing time zone offset with name using both `%z` and `%Z`

## [v0.1.4](https://github.com/itchyny/timefmt-go/compare/v0.1.3..v0.1.4) (2022-09-01)
* improve documents
* drop support for Go 1.16

## [v0.1.3](https://github.com/itchyny/timefmt-go/compare/v0.1.2..v0.1.3) (2021-04-14)
* implement `ParseInLocation` for configuring the default location

## [v0.1.2](https://github.com/itchyny/timefmt-go/compare/v0.1.1..v0.1.2) (2021-02-22)
* implement parsing/formatting time zone offset with colons (`%:z`, `%::z`, `%:::z`)
* recognize `Z` as UTC on parsing time zone offset (`%z`)
* fix padding on formatting time zone offset (`%z`)

## [v0.1.1](https://github.com/itchyny/timefmt-go/compare/v0.1.0..v0.1.1) (2020-09-01)
* fix overflow check in 32-bit architecture

## [v0.1.0](https://github.com/itchyny/timefmt-go/compare/2c02364..v0.1.0) (2020-08-16)
* initial implementation
