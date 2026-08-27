# paid-twice.awk — flag payments that look like doubles: same payee, same
# amount, within WINDOW days (default 7). CSV: date(YYYY-MM-DD),payee,amount
BEGIN { FS=","; WINDOW = (WINDOW=="" ? 7 : WINDOW) }
function daynum(d,   y,m,dd) {
  y=substr(d,1,4)+0; m=substr(d,6,2)+0; dd=substr(d,9,2)+0
  if (m<=2) { y--; m+=12 }
  return int(365.25*y)+int(30.6001*(m+1))+dd
}
NR>1 && NF>=3 {
  gsub(/^[ \t]+|[ \t]+$/, "", $2); amt=$3+0
  key=$2 "|" amt
  if (key in last && daynum($1)-daynum(last[key]) <= WINDOW && daynum($1)-daynum(last[key]) >= 0)
    printf "possible double: %s — %s on %s and again on %s\n", $2, $3, last[key], $1
  last[key]=$1
}
