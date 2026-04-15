package timefmt

import (
	"strconv"
	"time"
)

// Format time to string using the format.
func Format(t time.Time, format string) string {
	return string(AppendFormat(make([]byte, 0, 64), t, format))
}

// AppendFormat appends formatted time string to the buffer.
func AppendFormat(buf []byte, t time.Time, format string) []byte {
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	var width, colons int
	var padding byte
	var pending string
	var upper, swap bool
	for i := 0; i < len(format); i++ {
		if b := format[i]; b == '%' {
			if i++; i == len(format) {
				buf = append(buf, '%')
				break
			}
			b, width, padding, upper, swap = format[i], 0, '0', false, false
		L:
			switch b {
			case '-':
				if pending != "" {
					buf = append(buf, '-')
					break
				}
				if i++; i == len(format) {
					goto K
				}
				padding = ^paddingMask
				b = format[i]
				goto L
			case '_':
				if i++; i == len(format) {
					goto K
				}
				padding = ' ' | ^paddingMask
				b = format[i]
				goto L
			case '^':
				if i++; i == len(format) {
					goto K
				}
				upper = true
				b = format[i]
				goto L
			case '#':
				if i++; i == len(format) {
					goto K
				}
				swap = true
				b = format[i]
				goto L
			case '0':
				if i++; i == len(format) {
					goto K
				}
				padding = '0' | ^paddingMask
				b = format[i]
				goto L
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				width = int(b & 0x0F)
				for i++; i < len(format); i++ {
					if b = format[i]; b <= '9' && '0' <= b {
						width = min(width*10+int(b&0x0F), 1024)
					} else {
						break
					}
				}
				if padding == ^paddingMask {
					padding = ' ' | ^paddingMask
				}
				if i == len(format) {
					goto K
				}
				goto L
			case 'Y':
				buf = appendInt(buf, year, or(width, 4), padding)
			case 'y':
				buf = appendInt(buf, abs(year%100), max(width, 2), padding)
			case 'C':
				c := year / 100
				z := year < 0 && c == 0
				if z {
					c = -1
				}
				buf = appendInt(buf, c, max(width, 2), padding)
				if z {
					buf[len(buf)-1] = '0'
				}
			case 'g':
				year, _ := t.ISOWeek()
				buf = appendInt(buf, abs(year%100), max(width, 2), padding)
			case 'G':
				year, _ := t.ISOWeek()
				buf = appendInt(buf, year, or(width, 4), padding)
			case 'm':
				buf = appendInt(buf, int(month), max(width, 2), padding)
			case 'B':
				buf = appendString(buf, longMonthNames[month-1], width, padding, upper, swap)
			case 'b', 'h':
				buf = appendString(buf, shortMonthNames[month-1], width, padding, upper, swap)
			case 'A':
				buf = appendString(buf, longWeekNames[t.Weekday()], width, padding, upper, swap)
			case 'a':
				buf = appendString(buf, shortWeekNames[t.Weekday()], width, padding, upper, swap)
			case 'w':
				buf = appendInt(buf, int(t.Weekday()), width, padding)
			case 'u':
				buf = appendInt(buf, or(int(t.Weekday()), 7), width, padding)
			case 'V':
				_, week := t.ISOWeek()
				buf = appendInt(buf, week, max(width, 2), padding)
			case 'U':
				week := (t.YearDay() + 6 - int(t.Weekday())) / 7
				buf = appendInt(buf, week, max(width, 2), padding)
			case 'W':
				week := t.YearDay()
				if int(t.Weekday()) > 0 {
					week -= int(t.Weekday()) - 7
				}
				week /= 7
				buf = appendInt(buf, week, max(width, 2), padding)
			case 'e':
				if padding < ^paddingMask {
					padding = ' '
				}
				fallthrough
			case 'd':
				buf = appendInt(buf, day, max(width, 2), padding)
			case 'j':
				buf = appendInt(buf, t.YearDay(), max(width, 3), padding)
			case 'k':
				if padding < ^paddingMask {
					padding = ' '
				}
				fallthrough
			case 'H':
				buf = appendInt(buf, hour, max(width, 2), padding)
			case 'l':
				if padding < ^paddingMask {
					padding = ' '
				}
				fallthrough
			case 'I':
				buf = appendInt(buf, or(hour%12, 12), max(width, 2), padding)
			case 'P':
				swap = !(upper || swap)
				fallthrough
			case 'p':
				if hour < 12 {
					buf = appendString(buf, "AM", width, padding, upper, swap)
				} else {
					buf = appendString(buf, "PM", width, padding, upper, swap)
				}
			case 'M':
				buf = appendInt(buf, minute, max(width, 2), padding)
			case 'S':
				buf = appendInt(buf, second, max(width, 2), padding)
			case 's':
				if padding < ^paddingMask {
					padding = ' '
				}
				buf = appendInt64(buf, t.Unix(), width, padding)
			case 'f':
				buf = appendInt(buf, t.Nanosecond()/1000, or(width, 6), padding)
			case 'Z', 'z':
				name, offset := t.Zone()
				if b == 'Z' && name != "" {
					buf = appendString(buf, name, width, padding, upper, swap)
					break
				}
				i := len(buf)
				if padding != ^paddingMask {
					for ; width > 1; width-- {
						buf = append(buf, padding&paddingMask)
					}
				}
				j := len(buf)
				if offset < 0 {
					buf = append(buf, '-')
					offset = -offset
				} else {
					buf = append(buf, '+')
				}
				k := len(buf)
				buf = appendInt(buf, offset/3600, 2, padding)
				if buf[k] == ' ' {
					buf[k-1], buf[k] = buf[k], buf[k-1]
				}
				if offset %= 3600; colons <= 2 || offset != 0 {
					if colons != 0 {
						buf = append(buf, ':')
					}
					buf = appendInt(buf, offset/60, 2, '0')
					if offset %= 60; colons == 2 || colons == 3 && offset != 0 {
						buf = append(buf, ':')
						buf = appendInt(buf, offset, 2, '0')
					}
				}
				colons = 0
				if k = min(len(buf)-j-1, j-i); k > 0 {
					copy(buf[j-k:], buf[j:])
					buf = buf[:len(buf)-k]
					if padding&paddingMask == '0' {
						buf[i], buf[j-k] = buf[j-k], buf[i]
					}
				}
			case ':':
				if pending != "" {
					buf = append(buf, ':')
				} else {
					colons = 1
				M:
					for i++; i < len(format); i++ {
						switch format[i] {
						case ':':
							colons++
						case 'z':
							if colons > 3 {
								i++
								break M
							}
							b = 'z'
							goto L
						default:
							break M
						}
					}
					buf = appendLast(buf, format[:i], width, padding)
					i--
					colons = 0
				}
			case 't':
				buf = appendString(buf, "\t", width, padding, false, false)
			case 'n':
				buf = appendString(buf, "\n", width, padding, false, false)
			case '%':
				buf = appendString(buf, "%", width, padding, false, false)
			case 'c':
				pending, swap = "a b e H:M:S Y", false
			case '+':
				pending, swap = "a b e H:M:S Z Y", false
			case 'v':
				pending, swap = "e-b-Y", false
			case 'r':
				pending, swap = "I:M:S p", false
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
					buf = appendLast(buf, format[:i], width-1, padding)
				}
				buf = append(buf, b)
			}
			if pending != "" {
				b, pending, width, padding = pending[0], pending[1:], 0, '0'
				goto L
			}
		} else {
			buf = append(buf, b)
		}
	}
	return buf
K:
	return appendLast(buf, format, width, padding)
}

const smalls = "" +
	"00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

func appendInt(buf []byte, num, width int, padding byte) []byte {
	var digits int
	switch {
	default:
		fallthrough
	case num < 0:
		return appendInt64(buf, int64(num), width, padding)
	case num < 100 && width == 2 && padding == '0':
		return append(buf, smalls[num*2:num*2+2]...)
	case num < 10:
		digits = 1
	case num < 100:
		digits = 2
	case num < 1000:
		digits = 3
	case num < 10000:
		digits = 4
	}
	if padding != ^paddingMask {
		padding &= paddingMask
		for ; width > digits; width-- {
			buf = append(buf, padding)
		}
	}
	switch digits {
	case 4:
		buf = append(buf, byte(num/1000)|'0')
		num %= 1000
		fallthrough
	case 3:
		buf = append(buf, byte(num/100)|'0')
		num %= 100
		fallthrough
	case 2:
		buf = append(buf, byte(num/10)|'0')
		num %= 10
		fallthrough
	default:
		return append(buf, byte(num)|'0')
	}
}

func appendInt64(buf []byte, num int64, width int, padding byte) []byte {
	if padding == ^paddingMask {
		return strconv.AppendInt(buf, num, 10)
	}
	padding &= paddingMask
	i := len(buf)
	for ; width > 1; width-- {
		buf = append(buf, padding)
	}
	j := len(buf)
	buf = strconv.AppendInt(buf, num, 10)
	if k := min(len(buf)-j-1, j-i); k > 0 {
		copy(buf[j-k:], buf[j:])
		buf = buf[:len(buf)-k]
		if j -= k; buf[i] == '0' && buf[j] == '-' {
			buf[i], buf[j] = '-', '0'
		}
	}
	return buf
}

func appendString(buf []byte, str string, width int, padding byte, upper, swap bool) []byte {
	if width > len(str) && padding != ^paddingMask {
		if padding < ^paddingMask {
			padding = ' '
		} else {
			padding &= paddingMask
		}
		for width -= len(str); width > 0; width-- {
			buf = append(buf, padding)
		}
	}
	switch {
	case swap:
		if str[1] < 'a' {
			for _, b := range []byte(str) {
				buf = append(buf, b|0x20)
			}
			break
		}
		fallthrough
	case upper:
		for _, b := range []byte(str) {
			buf = append(buf, b&0x5F)
		}
	default:
		buf = append(buf, str...)
	}
	return buf
}

func appendLast(buf []byte, format string, width int, padding byte) []byte {
	for i := len(format) - 1; i >= 0; i-- {
		if format[i] == '%' {
			buf = appendString(buf, format[i:], width, padding, false, false)
			break
		}
	}
	return buf
}

func or(x, y int) int {
	if x != 0 {
		return x
	}
	return y
}

func abs(x int) int {
	if x >= 0 {
		return x
	}
	return -x
}

const paddingMask byte = 0x7F

var longMonthNames = []string{
	"January",
	"February",
	"March",
	"April",
	"May",
	"June",
	"July",
	"August",
	"September",
	"October",
	"November",
	"December",
}

var shortMonthNames = []string{
	"Jan",
	"Feb",
	"Mar",
	"Apr",
	"May",
	"Jun",
	"Jul",
	"Aug",
	"Sep",
	"Oct",
	"Nov",
	"Dec",
}

var longWeekNames = []string{
	"Sunday",
	"Monday",
	"Tuesday",
	"Wednesday",
	"Thursday",
	"Friday",
	"Saturday",
}

var shortWeekNames = []string{
	"Sun",
	"Mon",
	"Tue",
	"Wed",
	"Thu",
	"Fri",
	"Sat",
}
