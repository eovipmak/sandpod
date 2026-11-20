#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
    echo "Run this installer as root." >&2
    exit 1
fi

dnf install -y https://rpms.remirepo.net/enterprise/remi-release-10.rpm
dnf install -y \
    php82-php-cli php82-php-fpm php82-php-mbstring php82-php-mysqlnd \
    php82-php-opcache php82-php-xml \
    php83-php-cli php83-php-fpm php83-php-mbstring php83-php-mysqlnd \
    php83-php-opcache php83-php-xml \
    php84-php-cli php84-php-fpm php84-php-mbstring php84-php-mysqlnd \
    php84-php-opcache php84-php-xml \
    php85-php-cli php85-php-fpm php85-php-mbstring php85-php-mysqlnd \
    php85-php-opcache php85-php-xml

if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
    if ! command -v semanage >/dev/null; then
        echo "Install policycoreutils-python-utils to label shared runtime paths." >&2
        exit 1
    fi

    for path in '/opt/remi(/.*)?' '/etc/opt/remi(/.*)?' '/usr/lib64/libonig\.so\.105'; do
        semanage fcontext -a -t container_file_t "$path" \
            || semanage fcontext -m -t container_file_t "$path"
    done
    restorecon -RF /opt/remi /etc/opt/remi /usr/lib64/libonig.so.105
fi
