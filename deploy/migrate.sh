#!/bin/sh
# Runs INSIDE a one-shot `postgres:16-alpine` pod started by the Jenkins stage 'Di trú CSDL'.
#
# Input (environment): DATABASE_DSN from Secret `vihat-miniapp-bi-mat`; MIG_LIST = space-separated
# migration file names in order; MIG_<n> = base64 of the n-th file (1-based, same order).
#
# WHY A LEDGER TABLE: the migrations are not all re-runnable — 0002 refuses a second run on purpose
# ("0002 da chay roi"). Each build must therefore apply only the files not applied yet, and
# `schema_migrations` is the record of which those are.
#
# FAIL CLOSED ON A DATABASE WITH TABLES BUT NO LEDGER: that database was migrated by hand
# (`make migrate`) and nothing here can know how far. Guessing would either re-run 0002 (refused) or
# skip a file that never ran. Stop and say how to record it.
set -eu

: "${DATABASE_DSN:?missing DATABASE_DSN}"
psqlq() { psql "$DATABASE_DSN" -X -q -v ON_ERROR_STOP=1 "$@"; }

ledger_existed=$(psqlq -At -c "SELECT to_regclass('schema_migrations') IS NOT NULL")
psqlq -c "CREATE TABLE IF NOT EXISTS schema_migrations (
  name       text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
)"

if [ "$ledger_existed" = "f" ]; then
  has_schema=$(psqlq -At -c "SELECT to_regclass('nhat_ky_dang_nhap') IS NOT NULL")
  if [ "$has_schema" = "t" ]; then
    echo "CSDL ĐÃ CÓ BẢNG (nhat_ky_dang_nhap) NHƯNG CHƯA CÓ SỔ schema_migrations."
    echo "Lược đồ từng được chạy tay; không đoán được tới tệp nào. Ghi tay những tệp ĐÃ chạy, ví dụ:"
    echo "  INSERT INTO schema_migrations(name) VALUES ('0001_init.sql'),('0002_nhat_ky_90_ngay_va_an_danh.sql');"
    echo "rồi bấm lại job."
    exit 3
  fi
fi

i=0
for name in $MIG_LIST; do
  i=$((i + 1))
  done_already=$(psqlq -At -c "SELECT count(*) FROM schema_migrations WHERE name = '$name'")
  if [ "$done_already" != "0" ]; then
    echo "đã có  $name"
    continue
  fi
  eval "b64=\${MIG_$i}"
  echo "áp     $name"
  printf '%s' "$b64" | base64 -d | psqlq -f -
  psqlq -c "INSERT INTO schema_migrations(name) VALUES ('$name')"
done
echo "XONG DI TRÚ"
