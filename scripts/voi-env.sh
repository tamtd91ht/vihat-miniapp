#!/bin/sh
# voi-env.sh — chạy một lệnh với cấu hình của MÁY LOCAL trong .env.local.
#
# VÌ SAO NẰM Ở ĐÂY CHỨ KHÔNG NẰM TRONG NHỊ PHÂN. Trên cụm, giá trị đến từ Secret
# và ConfigMap; một nhị phân biết tự đọc tệp cấu hình là một nhị phân có ĐƯỜNG
# NẠP THỨ HAI mà không ai kiểm — và cái ngày một tệp .env lạc vào ảnh container
# là cái ngày nó lặng lẽ đè cấu hình của cụm. internal/config vì thế vẫn đọc
# đúng một nguồn: môi trường. Việc dựng môi trường ấy trên máy local là việc của
# tệp này.
#
# THỨ TỰ ƯU TIÊN — BIẾN SHELL ĐÈ TỆP:
#   biến đã có trong môi trường  >  dòng trong .env.local  >  không có gì
# Nhờ thế `DATABASE_DSN=... make migrate` chạy được một lần với DSN khác mà
# không phải sửa tệp, và cũng không phải nhớ sửa lại.
#
# KHÔNG BAO GIỜ IN GIÁ TRỊ. Chỉ in TÊN biến. Tệp này chứa secret key thật của
# Mini App và DSN có mật khẩu; một dòng `echo` ở đây là secret nằm trong
# scrollback của terminal và trong log của CI.
#
#   ./scripts/voi-env.sh go run ./cmd/server
#   TEP_ENV=.env.staging.local ./scripts/voi-env.sh go run ./cmd/server

set -eu

TEP="${TEP_ENV:-.env.local}"

if [ "$#" -eq 0 ]; then
	echo "voi-env.sh: thiếu lệnh cần chạy" >&2
	exit 2
fi

if [ -f "$TEP" ]; then
	da_nap=""
	da_bo=""

	# `|| [ -n "$dong" ]` để không nuốt mất dòng cuối khi tệp không kết thúc
	# bằng ký tự xuống dòng.
	while IFS= read -r dong || [ -n "$dong" ]; do
		# CRLF: tệp soạn bằng trình soạn thảo Windows để lại \r ở cuối dòng, và
		# một \r dính vào cuối DSN tạo ra lỗi kết nối không ai đoán ra nổi — nó
		# không hiện lên màn hình.
		dong="$(printf '%s' "$dong" | tr -d '\r')"

		case "$dong" in
		'' | '#'*) continue ;;
		*=*) ;;
		*) continue ;;
		esac

		ten="${dong%%=*}"
		gia="${dong#*=}"
		ten="$(printf '%s' "$ten" | tr -d ' 	')"

		case "$ten" in
		'' | *[!A-Za-z0-9_]*)
			echo ">> $TEP: bỏ qua một dòng có tên biến không hợp lệ" >&2
			continue
			;;
		esac

		# BIẾN SHELL ĐÈ TỆP.
		if eval "[ -n \"\${$ten+co}\" ]"; then
			da_bo="$da_bo $ten"
			continue
		fi

		# Bỏ cặp nháy bao ngoài nếu người dùng có gõ.
		case "$gia" in
		'"'*'"')
			gia="${gia#\"}"
			gia="${gia%\"}"
			;;
		"'"*"'")
			gia="${gia#\'}"
			gia="${gia%\'}"
			;;
		esac

		export "$ten=$gia"
		da_nap="$da_nap $ten"
	done <"$TEP"

	# CHỈ TÊN BIẾN, không giá trị. Viết bằng `if` chứ không bằng `[ ... ] && ...`:
	# dưới `set -e`, một `[` trả về sai là đủ để script thoát giữa chừng.
	if [ -n "$da_nap" ]; then
		echo ">> nạp từ $TEP:$da_nap" >&2
	fi
	if [ -n "$da_bo" ]; then
		echo ">> biến shell đè $TEP (giữ giá trị đang có):$da_bo" >&2
	fi
else
	echo ">> không có $TEP — dùng nguyên môi trường hiện tại" >&2
fi

exec "$@"
