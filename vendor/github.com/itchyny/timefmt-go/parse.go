package timefmt

import (
	"errors"
	"fmt"
	"time"
)

// Parse time string using the format.
func Parse(source, format string) (t time.Time, err error) {
	return parse(source, format, time.UTC, time.Local)
}

// ParseInLocation parses time string with the default location.
// The location is also used to parse the time zone name (%Z).
func ParseInLocation(source, format string, loc *time.Location) (t time.Time, err error) {
	return parse(source, format, loc, loc)
}

func parse(source, format string, loc, base *time.Location) (t time.Time, err error) {
	year, month, day, hour, minute, second, nanosecond := 1900, 1, 0, 0, 0, 0, 0
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to parse %q with %q: %w", source, format, err)
		}
	}()
	var j, week, weekday, yday, colons, sign int
	century, weekstart := -1, time.Weekday(-1)
	var pm, hasISOYear, hasZoneName, hasZoneOffset bool
	var pending string
	for i, l := 0, len(source); i < len(format); i++ {
		if b := format[i]; b == '%' {
			if i++; i == len(format) {
				err = errors.New(`stray "%"`)
				return
			}
			b = format[i]
		L:
			switch b {
			case 'G':
				hasISOYear = true
				fallthrough
			case 'Y':
				sign, j = parseSign(source, j, l)
				if year, j, err = parseInt(source, j, 4, 0, 9999, b); err != nil {
					return
				}
				year *= sign
			case 'g':
				hasISOYear = true
				fallthrough
			case 'y':
				if year, j, err = parseInt(source, j, 2, 0, 99, b); err != nil {
					return
				}
				if year < 69 {
					year += 2000
				} else {
					year += 1900
				}
			case 'C':
				sign, j = parseSign(source, j, l)
				if sign < 0 {
					err = errors.New(`negative century is not supported for "%C"`)
					return
				}
				if century, j, err = parseInt(source, j, 2, 0, 99, 'C'); err != nil {
					return
				}
			case 'm':
				if month, j, err = parseInt(source, j, 2, 1, 12, 'm'); err != nil {
					return
				}
			case 'B':
				if month, j, err = parseAny(source, j, longMonthNames, 'B'); err != nil {
					return
				}
			case 'b', 'h':
				if month, j, err = parseAny(source, j, shortMonthNames, b); err != nil {
					return
				}
			case 'A':
				if weekday, j, err = parseAny(source, j, longWeekNames, 'A'); err != nil {
					return
				}
			case 'a':
				if weekday, j, err = parseAny(source, j, shortWeekNames, 'a'); err != nil {
					return
				}
			case 'w':
				if weekday, j, err = parseInt(source, j, 1, 0, 6, 'w'); err != nil {
					return
				}
				weekday++
			case 'u':
				if weekday, j, err = parseInt(source, j, 1, 1, 7, 'u'); err != nil {
					return
				}
				weekday = weekday%7 + 1
			case 'V':
				if week, j, err = parseInt(source, j, 2, 1, 53, b); err != nil {
					return
				}
				weekstart = time.Thursday
				weekday = or(weekday, 2)
			case 'U':
				if week, j, err = parseInt(source, j, 2, 0, 53, b); err != nil {
					return
				}
				weekstart = time.Sunday
				weekday = or(weekday, 1)
			case 'W':
				if week, j, err = parseInt(source, j, 2, 0, 53, b); err != nil {
					return
				}
				weekstart = time.Monday
				weekday = or(weekday, 2)
			case 'e':
				if j < l && source[j] == ' ' {
					j++
				}
				fallthrough
			case 'd':
				if day, j, err = parseInt(source, j, 2, 1, 31, b); err != nil {
					return
				}
			case 'j':
				if yday, j, err = parseInt(source, j, 3, 1, 366, 'j'); err != nil {
					return
				}
			case 'k':
				if j < l && source[j] == ' ' {
					j++
				}
				fallthrough
			case 'H':
				if hour, j, err = parseInt(source, j, 2, 0, 23, b); err != nil {
					return
				}
			case 'l':
				if j < l && source[j] == ' ' {
					j++
				}
				fallthrough
			case 'I':
				if hour, j, err = parseInt(source, j, 2, 1, 12, b); err != nil {
					return
				}
				if hour == 12 {
					hour = 0
				}
			case 'P', 'p':
				var ampm int
				if ampm, j, err = parseAny(source, j, []string{"AM", "PM"}, b); err != nil {
					return
				}
				pm = ampm == 2
			case 'M':
				if minute, j, err = parseInt(source, j, 2, 0, 59, 'M'); err != nil {
					return
				}
			case 'S':
				if second, j, err = parseInt(source, j, 2, 0, 60, 'S'); err != nil {
					return
				}
			case 's':
				sign, j = parseSign(source, j, l)
				var unix int64
				if unix, j, err = parseInt64(source, j, 19, 's'); err != nil {
					return
				}
				t = time.Unix(int64(sign)*unix, 0).In(time.UTC)
				var mon time.Month
				year, mon, day = t.Date()
				hour, minute, second = t.Clock()
				month = int(mon)
			case 'f':
				microsecond, i := 0, j
				if microsecond, j, err = parseInt(source, j, 6, 0, 999999, 'f'); err != nil {
					return
				}
				for i = j - i; i < 6; i++ {
					microsecond *= 10
				}
				nanosecond = microsecond * 1000
			case 'Z':
				i := j
				for ; j < l; j++ {
					if c := source[j]; c < 'A' || 'Z' < c {
						break
					}
				}
				t, err = time.ParseInLocation("MST", source[i:j], base)
				if err != nil {
					err = fmt.Errorf(`cannot parse %q with "%%Z"`, source[i:j])
					return
				}
				if hasZoneOffset {
					name, _ := t.Zone()
					_, offset := locationZone(loc)
					loc = time.FixedZone(name, offset)
				} else {
					loc = t.Location()
				}
				hasZoneName = true
			case 'z':
				if j >= l {
					err = parseZFormatError(colons)
					return
				}
				sign = 1
				switch source[j] {
				case '-':
					sign = -1
					fallthrough
				case '+':
					hour, minute, second, i := 0, 0, 0, j+1
					if hour, j, _ = parseInt(source, i, 2, 0, 23, 'z'); j != i+2 {
						err = parseZFormatError(colons)
						return
					}
					if j >= l || source[j] != ':' {
						if colons > 0 && colons < 3 {
							err = expectedColonForZFormatError(colons)
							return
						}
					} else if j++; colons == 0 {
						colons = 4
					}
					i = j
					if minute, j, _ = parseInt(source, i, 2, 0, 59, 'z'); j != i+2 {
						if colons > 0 && colons != 3 {
							err = parseZFormatError(colons & 3)
							return
						}
						j = i
					} else if colons > 1 {
						if j >= l || source[j] != ':' {
							if colons < 3 {
								err = expectedColonForZFormatError(colons)
								return
							}
						} else {
							i = j + 1
							if second, j, _ = parseInt(source, i, 2, 0, 59, 'z'); j != i+2 {
								if colons < 3 {
									err = parseZFormatError(colons)
									return
								}
								j = i - 1
							}
						}
					}
					var name string
					if hasZoneName {
						name, _ = locationZone(loc)
					}
					loc, colons = time.FixedZone(name, sign*((hour*60+minute)*60+second)), 0
					hasZoneOffset = true
				case 'Z':
					loc, colons, j = time.UTC, 0, j+1
				default:
					err = parseZFormatError(colons)
					return
				}
			case ':':
				if pending != "" {
					if j >= l || source[j] != b {
						err = expectedFormatError(b)
						return
					}
					j++
				} else {
					for colons = 1; colons <= 3; colons++ {
						if i++; i == len(format) {
							break
						} else if b = format[i]; b == 'z' {
							goto L
						} else if b != ':' || colons == 3 {
							break
						}
					}
					err = expectedZAfterColonError(colons)
					return
				}
			case 't', 'n':
				i := j
			K:
				for ; j < l; j++ {
					switch source[j] {
					case ' ', '\t', '\n', '\v', '\f', '\r':
					default:
						break K
					}
				}
				if i == j {
					err = fmt.Errorf(`expected a space for "%%%c"`, b)
					return
				}
			case '%':
				if j >= l || source[j] != b {
					err = expectedFormatError(b)
					return
				}
				j++
			case 'c':
				pending = "a b e H:M:S Y"
			case '+':
				pending = "a b e H:M:S Z Y"
			case 'v':
				pending = "e-b-Y"
			case 'r':
				pending = "I:M:S p"
			case 'F':
				pending = "Y-m-d"
			case 'D', 'x':
				pending = "m/d/y"
			case 'T', 'X':
				pending = "H:M:S"
			case 'R':
				pending = "H:M"
			default:
				if pending == "" {
					err = fmt.Errorf(`unexpected format "%%%c"`, b)
					return
				}
				if j >= l || source[j] != b {
					err = expectedFormatError(b)
					return
				}
				j++
			}
			if pending != "" {
				b, pending = pending[0], pending[1:]
				goto L
			}
		} else if j >= l || source[j] != b {
			err = expectedFormatError(b)
			return
		} else {
			j++
		}
	}
	if j < len(source) {
		err = fmt.Errorf("unparsed string %q", source[j:])
		return
	}
	if pm {
		hour += 12
	}
	if century >= 0 {
		year = century*100 + year%100
	}
	if day == 0 {
		if yday > 0 {
			if hasISOYear {
				err = errors.New(`use "%Y" to parse non-ISO year for "%j"`)
				return
			}
			return time.Date(year, time.January, yday, hour, minute, second, nanosecond, loc), nil
		}
		if weekstart >= time.Sunday {
			if weekstart == time.Thursday {
				if !hasISOYear {
					err = errors.New(`use "%G" to parse ISO year for "%V"`)
					return
				}
			} else if hasISOYear {
				err = errors.New(`use "%Y" to parse non-ISO year for "%U" or "%W"`)
				return
			}
			if weekstart > time.Sunday && weekday == 1 {
				week++
			}
			t := time.Date(year, time.January, -int(weekstart), hour, minute, second, nanosecond, loc)
			return t.AddDate(0, 0, week*7-int(t.Weekday())+weekday-1), nil
		}
		day = 1
	}
	return time.Date(year, time.Month(month), day, hour, minute, second, nanosecond, loc), nil
}

func locationZone(loc *time.Location) (name string, offset int) {
	return time.Date(2000, time.January, 1, 0, 0, 0, 0, loc).Zone()
}

type parseFormatError byte

func (err parseFormatError) Error() string {
	return fmt.Sprintf(`cannot parse "%%%c"`, byte(err))
}

type expectedFormatError byte

func (err expectedFormatError) Error() string {
	return fmt.Sprintf("expected %q", byte(err))
}

type parseZFormatError int

func (err parseZFormatError) Error() string {
	return `cannot parse "%` + `:::z"`[3-err:]
}

type expectedColonForZFormatError int

func (err expectedColonForZFormatError) Error() string {
	return `expected ':' for "%` + `:::z"`[3-err:]
}

type expectedZAfterColonError int

func (err expectedZAfterColonError) Error() string {
	return `expected 'z' after "%` + `:::"`[3-err:]
}

func parseSign(source string, index, l int) (int, int) {
	if index < l && source[index] == '-' {
		return -1, index + 1
	}
	return 1, index
}

// parseInt parses an integer from source. This is intentionally not a
// wrapper of parseInt64 to avoid the overhead of int64 arithmetic and
// function call indirection in the hot path (~20% slower in benchmarks).
func parseInt(source string, index, size, minimum, maximum int, format byte) (int, int, error) {
	var value int
	i := index
	for size = min(i+size, len(source)); i < size; i++ {
		if b := source[i] - '0'; b < 10 {
			value = value*10 + int(b)
		} else {
			break
		}
	}
	if i == index || value < minimum || maximum < value {
		return 0, 0, parseFormatError(format)
	}
	return value, i, nil
}

func parseInt64(source string, index, size int, format byte) (int64, int, error) {
	var value int64
	i := index
	for size = min(i+size, len(source)); i < size; i++ {
		if b := source[i] - '0'; b < 10 {
			value = value*10 + int64(b)
		} else {
			break
		}
	}
	if i == index {
		return 0, 0, parseFormatError(format)
	}
	return value, i, nil
}

func parseAny(source string, index int, candidates []string, format byte) (int, int, error) {
L:
	for i, xs := range candidates {
		j := index
		for k := 0; k < len(xs); k, j = k+1, j+1 {
			if j >= len(source) {
				continue L
			}
			if x, y := xs[k], source[j]; x != y && x|0x20 != y|0x20 {
				continue L
			}
		}
		return i + 1, j, nil
	}
	return 0, 0, parseFormatError(format)
}
