package policy

import "github.com/open-policy-agent/opa/v1/ast"

func builtins() []*ast.Builtin {
	b := []*ast.Builtin{
		ast.Equality,

		// Assignment (":=")
		ast.Assign,

		// Membership, infix "in": `x in xs`
		ast.Member,
		ast.MemberWithKey,

		// Comparisons
		ast.GreaterThan,
		ast.GreaterThanEq,
		ast.LessThan,
		ast.LessThanEq,
		ast.NotEqual,
		ast.Equal,

		// Arithmetic
		ast.Plus,
		ast.Minus,
		ast.Multiply,
		ast.Divide,
		ast.Ceil,
		ast.Floor,
		ast.Round,
		ast.Abs,
		ast.Rem,

		// Bitwise Arithmetic
		ast.BitsOr,
		ast.BitsAnd,
		ast.BitsNegate,
		ast.BitsXOr,
		ast.BitsShiftLeft,
		ast.BitsShiftRight,

		// Binary
		ast.And,
		ast.Or,

		// Aggregates
		ast.Count,
		ast.Sum,
		ast.Product,
		ast.Max,
		ast.Min,
		ast.Any,
		ast.All,

		// Arrays
		ast.ArrayConcat,
		ast.ArraySlice,
		ast.ArrayReverse,

		// Conversions
		ast.ToNumber,

		// Casts (DEPRECATED)
		ast.CastObject,
		ast.CastNull,
		ast.CastBoolean,
		ast.CastString,
		ast.CastSet,
		ast.CastArray,

		// Regular Expressions
		ast.RegexIsValid,
		ast.RegexMatch,
		ast.RegexMatchDeprecated,
		ast.RegexSplit,
		ast.GlobsMatch,
		ast.RegexTemplateMatch,
		ast.RegexFind,
		ast.RegexFindAllStringSubmatch,
		ast.RegexReplace,

		// Sets
		ast.SetDiff,
		ast.Intersection,
		ast.Union,

		// Strings
		ast.AnyPrefixMatch,
		ast.AnySuffixMatch,
		ast.Concat,
		ast.FormatInt,
		ast.IndexOf,
		ast.IndexOfN,
		ast.Substring,
		ast.Lower,
		ast.Upper,
		ast.Contains,
		ast.StringCount,
		ast.StartsWith,
		ast.EndsWith,
		ast.Split,
		ast.Replace,
		ast.ReplaceN,
		ast.Trim,
		ast.TrimLeft,
		ast.TrimPrefix,
		ast.TrimRight,
		ast.TrimSuffix,
		ast.TrimSpace,
		ast.Sprintf,
		ast.StringReverse,
		ast.RenderTemplate,

		// Numbers
		ast.NumbersRange,
		ast.NumbersRangeStep,
		ast.RandIntn,

		// Encoding
		ast.JSONMarshal,
		ast.JSONMarshalWithOptions,
		ast.JSONUnmarshal,
		ast.JSONIsValid,
		ast.Base64Encode,
		ast.Base64Decode,
		ast.Base64IsValid,
		ast.Base64UrlEncode,
		ast.Base64UrlEncodeNoPad,
		ast.Base64UrlDecode,
		ast.URLQueryDecode,
		ast.URLQueryEncode,
		ast.URLQueryEncodeObject,
		ast.URLQueryDecodeObject,
		ast.YAMLMarshal,
		ast.YAMLUnmarshal,
		ast.YAMLIsValid,
		ast.HexEncode,
		ast.HexDecode,

		// Object Manipulation
		ast.ObjectUnion,
		ast.ObjectUnionN,
		ast.ObjectRemove,
		ast.ObjectFilter,
		ast.ObjectGet,
		ast.ObjectKeys,
		ast.ObjectSubset,

		// JSON Object Manipulation
		ast.JSONFilter,
		ast.JSONRemove,
		ast.JSONPatch,

		// Time
		ast.NowNanos,
		ast.ParseNanos,
		ast.ParseRFC3339Nanos,
		ast.ParseDurationNanos,
		ast.Format,
		ast.Date,
		ast.Clock,
		ast.Weekday,
		ast.AddDate,
		ast.Diff,

		// Sort
		ast.Sort,

		// Types
		ast.IsNumber,
		ast.IsString,
		ast.IsBoolean,
		ast.IsArray,
		ast.IsSet,
		ast.IsObject,
		ast.IsNull,
		ast.TypeNameBuiltin,

		// Glob
		ast.GlobMatch,
		ast.GlobQuoteMeta,

		// Units
		ast.UnitsParse,
		ast.UnitsParseBytes,

		// UUIDs
		ast.UUIDRFC4122,
		ast.UUIDParse,

		// SemVers
		ast.SemVerIsValid,
		ast.SemVerCompare,

		// Printing
		ast.Print,
		ast.InternalPrint,
	}
	return b
}
