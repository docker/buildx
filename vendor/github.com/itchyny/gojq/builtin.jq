def map(f): [.[] | f];
def add(f): [f] | add;
def map_values(f): .[] |= f;
def in(xs): . as $x | xs | has($x);
def not: if . then false else true end;
def select(f): if f then . else empty end;

def recurse: recurse(.[]?);
def recurse(f): def r: ., (f | r); r;
def recurse(f; cond): def r: ., (f | select(cond) | r); r;

def to_entries: [keys[] as $k | {key: $k, value: .[$k]}];
def from_entries: map({ (.key // .Key // .name // .Name):
  if has("value") then .value else .Value end }) | add // {};
def with_entries(f): to_entries | map(f) | from_entries;

def while(cond; update):
  def _while: if cond then ., (update | _while) else empty end;
  _while;
def until(cond; next):
  def _until: if cond then . else next | _until end;
  _until;
def repeat(f):
  def _repeat: f, _repeat;
  _repeat;
def range($end): _range(0; $end; 1);
def range($start; $end): _range($start; $end; 1);
def range($start; $end; $step): _range($start; $end; $step);

def min_by(f): _min_by(map([f]));
def max_by(f): _max_by(map([f]));
def sort_by(f): _sort_by(map([f]));
def group_by(f): _group_by(map([f]));
def unique_by(f): _unique_by(map([f]));

def arrays: select(type == "array");
def objects: select(type == "object");
def iterables: select(type | . == "array" or . == "object");
def booleans: select(type == "boolean");
def numbers: select(type == "number");
def finites: select(isfinite);
def normals: select(isnormal);
def strings: select(type == "string");
def nulls: select(. == null);
def values: select(. != null);
def scalars: select(type | . != "array" and . != "object");

def combinations:
  if length == 0 then
    []
  else
    .[0][] as $x | [$x] + (.[1:] | combinations)
  end;
def combinations(n): [limit(n; repeat(.))] | combinations;

def walk(f):
  def _walk:
    if type == "array" then
      map(_walk)
    elif type == "object" then
      map_values(_walk)
    end | f;
  _walk;

def first: .[0];
def first(g): label $out | g | ., break $out;
def last: .[-1];
def last(g): _last(g);
def isempty(g): label $out | (g | false, break $out), true;
def all: all(.);
def all(y): all(.[]; y);
def all(g; y): isempty(g | select(y | not));
def any: any(.);
def any(y): any(.[]; y);
def any(g; y): isempty(g | select(y)) | not;

def limit($n; g):
  if $n > 0 then
    label $out |
    foreach g as $item (
      $n;
      . - 1;
      $item, if . <= 0 then break $out else empty end
    )
  elif $n == 0 then
    empty
  else
    error("limit doesn't support negative count")
  end;
def skip($n; g):
  if $n > 0 then
    foreach g as $item (
      $n;
      . - 1;
      if . < 0 then $item else empty end
    )
  elif $n == 0 then
    g
  else
    error("skip doesn't support negative count")
  end;
def nth($n): .[$n];
def nth($n; g):
  if $n >= 0 then
    first(skip($n; g))
  else
    error("nth doesn't support negative index")
  end;

def truncate_stream(f):
  . as $n | null | f |
  if .[0] | length > $n then .[0] |= .[$n:] else empty end;
def fromstream(f):
  foreach f as $pv (
    null;
    if .e then null end |
    $pv as [$p, $v] |
    if $pv | length == 2 then
      setpath(["v"] + $p; $v) |
      setpath(["e"]; $p | length == 0)
    else
      setpath(["e"]; $p | length == 1)
    end;
    if .e then .v else empty end
  );
def tostream:
  path(def r: (.[]? | r), .; r) as $p |
  getpath($p) |
  reduce path(.[]?) as $q ([$p, .]; [$p + $q]);

def del(f): delpaths([path(f)]);
def paths: path(..) | select(. != []);
def paths(f): path(.. | select(f)) | select(. != []);
def pick(f): . as $v |
  reduce path(f) as $p (null; setpath($p; $v | getpath($p)));

def fromdateiso8601: strptime("%Y-%m-%dT%H:%M:%S%z") | mktime;
def todateiso8601: strftime("%Y-%m-%dT%H:%M:%SZ");
def fromdate: fromdateiso8601;
def todate: todateiso8601;

def match($re): match($re; null);
def match($re; $flags): _match($re; $flags; false)[];
def test($re): test($re; null);
def test($re; $flags): _match($re; $flags; true);
def capture($re): capture($re; null);
def capture($re; $flags): match($re; $flags) | .captures | _captures;
def scan($re): scan($re; null);
def scan($re; $flags):
  match($re; $flags + "g") |
  if .captures == [] then
    .string
  else
    [.captures[].string]
  end;
def splits($re; $flags):
  .[foreach (match($re; $flags + "g"), null) as {$offset, $length}
      (null; {start: .next, end: $offset, next: $offset + $length})];
def splits($re): splits($re; null);
def split($re; $flags): [splits($re; $flags)];
def sub($re; str): sub($re; str; null);
def sub($re; str; $flags):
  reduce match($re; $flags) as {$offset, $length, $captures}
    ({s: ., r: []};
      reduce ($captures | _captures | str) as $s
        (.i = 0; .r[.i] += .s[.next:$offset] + $s | .i += 1) |
      .next = $offset + $length) | .r[] + .s[.next:] // .s;
def gsub($re; str): sub($re; str; "g");
def gsub($re; str; $flags): sub($re; str; $flags + "g");

def inputs:
  try
    repeat(input)
  catch
    if . == "break" then empty else error end;

def INDEX(stream; idx_expr):
  reduce stream as $row ({}; .[$row | idx_expr | tostring] = $row);
def INDEX(idx_expr):
  INDEX(.[]; idx_expr);
def JOIN($idx; idx_expr):
  [.[] | [., $idx[idx_expr]]];
def JOIN($idx; stream; idx_expr):
  stream | [., $idx[idx_expr]];
def JOIN($idx; stream; idx_expr; join_expr):
  stream | [., $idx[idx_expr]] | join_expr;
def IN(s): any(s == .; .);
def IN(src; s): any(src == s; .);
