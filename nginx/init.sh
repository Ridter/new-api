#!/bin/bash
# 检查输入参数
if [ "$#" -ne 2 ]; then
    echo "Usage: $0 domain proxy_phaas_server"
    echo " |_ example: $0 test.example.com https://8.8.8.8:51234"
    exit 1
fi

FULL_DOMAIN=$1
TARGET=$2

# 检查域名解析
if [ -z "$(dig +short "$FULL_DOMAIN")" ]; then
    echo "Error: Domain does not resolve"
    exit 1
fi

# 检查是否安装了Docker
if ! command -v docker >/dev/null; then
    echo "Docker is not installed. Do you want to install it? (yes/no)"
    read -r answer
    if [ "$answer" = "yes" ]; then
        curl -fsSL https://get.docker.com -o get-docker.sh
        sudo sh get-docker.sh
        rm get-docker.sh
    else
        echo "Docker is required. Exiting."
        exit 1
    fi
fi

# 检查是否安装了Docker Compose
if ! command -v docker-compose >/dev/null; then
    echo "Docker Compose is not installed. Do you want to install it? (yes/no)"
    read -r answer
    if [ "$answer" = "yes" ]; then
        # 获取最新版本
        COMPOSE_VERSION=$(curl -s https://api.github.com/repos/docker/compose/releases/latest | grep '"tag_name":' | cut -d '"' -f 4)
        sudo curl -L "https://github.com/docker/compose/releases/download/$COMPOSE_VERSION/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
        sudo chmod +x /usr/local/bin/docker-compose
    else
        echo "Docker Compose is required. Exiting."
        exit 1
    fi
fi

# 生成user_conf.d文件夹和配置文件
# 如果目录不存在，就创建目录
if [ ! -d "user_conf.d" ]; then
    mkdir -p user_conf.d
fi

# 根据输入域名，生成Nginx配置文件
cat > user_conf.d/$FULL_DOMAIN.conf <<EOL

# Add this outside of the server blocks
map \$remote_addr \$blocked {
    default 0;
    150.109.96.100 1;
    43.132.141.21 1;    # ??

}

map \$http_user_agent \$is_black_http_client {
    default 0;
    ~*Go-http-client/1\.1 1;
    ~*curl/ 1;
    ~*Wget/ 1;
    ~*Python-urllib/ 1;
    ~*Java/ 1;
    ~*libwww-perl/ 1;
    ~*Apache-HttpClient/ 1;
    # User-Agents containing 'bot', 'spider', 'crawl', etc., case insensitive
    "~*(bot|spider|crawl)" 1;
    # User-Agents containing specific identifiers, case insensitive
    "~*python-requests" 1;
    "~*slurp" 1;
    "~*duckduckgo" 1;
    "~*baiduspider" 1;
    "~*yandex" 1;    
    "~*facebook" 1;
    "~*twitterbot" 1;
    "~*linkedinbot" 1;
    "~*pinterest" 1;
}

server {
    listen 80;
    server_name $FULL_DOMAIN;
    location / {
        return 301 https://\$host\$request_uri;
    }
    location ^~ /.well-known/acme-challenge/ {
        root /var/www/letsencrypt;
    }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name $FULL_DOMAIN;

    ssl_certificate         /etc/letsencrypt/live/$FULL_DOMAIN/fullchain.pem;
    ssl_certificate_key     /etc/letsencrypt/live/$FULL_DOMAIN/privkey.pem;
    ssl_session_timeout 1d;
    ssl_session_cache shared:MozSSL:10m;  # about 40000 sessions
    ssl_session_tickets off;

    # openssl dhparam 1024 > /path/to/dhparam
    ssl_dhparam /path/to/dhparam;

    # old configuration
    ssl_protocols TLSv1 TLSv1.1 TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA256:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA;
    ssl_prefer_server_ciphers on;

    # 增加请求体大小限制，避免 413 错误
    client_max_body_size 100M;
    # 增加缓冲区大小，避免大请求写入临时文件 (Claude API 长对话历史)
    client_body_buffer_size 1M;
    # 增加代理缓冲区，提升性能
    proxy_buffering on;
    proxy_buffer_size 128k;
    proxy_buffers 8 256k;
    proxy_busy_buffers_size 512k;

    location / {
        proxy_redirect off;
        proxy_read_timeout 1200s;
        proxy_pass ${TARGET};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        # Config for 0-RTT in TLSv1.3
        proxy_set_header Early-Data \$ssl_early_data;
    }
}
EOL

# 生成openssl_conf文件夹和配置文件
# 如果目录不存在，就创建目录
if [ ! -d "openssl_conf" ]; then
    mkdir -p openssl_conf
fi

# 生成openssl配置文件
cat > openssl_conf/openssl.cnf <<EOL
openssl_conf = openssl_init

[openssl_init]
ssl_conf = ssl_sect

[ssl_sect]
system_default = system_default_sect

[system_default_sect]
MinProtocol = TLSv1
CipherString = ALL:@SECLEVEL=0
EOL

# 生成script文件夹和脚本文件
# 如果目录不存在，就创建目录
if [ ! -d "script" ]; then
    mkdir -p script
fi

# 生成run_certbot.sh脚本
cat << 'EOF' > script/run_certbot.sh
#!/bin/bash
set -e

# URLs used when requesting certificates.
# These are picked up from the environment if they are set, which enables
# advanced usage of custom ACME servers, else it will use the default Let's
# Encrypt servers defined here.
: "${CERTBOT_PRODUCTION_URL=https://acme-v02.api.letsencrypt.org/directory}"
: "${CERTBOT_STAGING_URL=https://acme-staging-v02.api.letsencrypt.org/directory}"

# Source in util.sh so we can have our nice tools.
. "$(cd "$(dirname "$0")"; pwd)/util.sh"

info "Starting certificate renewal process"
STATUS=-1
# We require an email to be able to request a certificate.
if [ -z "${CERTBOT_EMAIL}" ]; then
    error "CERTBOT_EMAIL environment variable undefined; certbot will do nothing!"
    exit 1
fi

# Use the correct challenge URL depending on if we want staging or not.
if [ "${STAGING}" = "1" ]; then
    debug "Using staging environment"
    letsencrypt_url="${CERTBOT_STAGING_URL}"
else
    debug "Using production environment"
    letsencrypt_url="${CERTBOT_PRODUCTION_URL}"
fi

# Ensure that an RSA key size is set.
if [ -z "${RSA_KEY_SIZE}" ]; then
    debug "RSA_KEY_SIZE unset, defaulting to 2048"
    RSA_KEY_SIZE=2048
fi

# Ensure that an elliptic curve is set.
if [ -z "${ELLIPTIC_CURVE}" ]; then
    debug "ELLIPTIC_CURVE unset, defaulting to 'secp256r1'"
    ELLIPTIC_CURVE="secp256r1"
fi

if [ "${1}" = "force" ]; then
    info "Forcing renewal of certificates"
    force_renew="--force-renewal"
fi

# Helper function to ask certbot to request a certificate for the given cert
# name. The CERTBOT_EMAIL environment variable must be defined, so that
# Let's Encrypt may contact you in case of security issues.
#
# $1: The name of the certificate (e.g. domain.rsa.dns-rfc2136)
# $2: String with all requested domains (e.g. -d domain.org -d www.domain.org)
# $3: Type of key algorithm to use (rsa or ecdsa)
# $4: The authenticator to use to solve the challenge
get_certificate() {
    local authenticator="${4,,}"
    local authenticator_params=""
    local challenge_type=""

    # Add correct parameters for the different authenticator types.
    if [ "${authenticator}" == "webroot" ]; then
        challenge_type="http-01"
        authenticator_params="--webroot-path=/var/www/letsencrypt"
    elif [[ "${authenticator}" == dns-* ]]; then
        challenge_type="dns-01"

        if [ "${authenticator#dns-}" == "route53" ]; then
            # This one is special and makes use of a different configuration.
            if [[ ( -z "${AWS_ACCESS_KEY_ID}" || -z "${AWS_SECRET_ACCESS_KEY}" ) && ! -f "${HOME}/.aws/config" ]]; then
                error "Authenticator is '${authenticator}' but neither '${HOME}/.aws/config' or AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY are found"
                return 1
            fi
        else
            local configfile="/etc/letsencrypt/${authenticator#dns-}.ini"
            if [ ! -f "${configfile}" ]; then
                error "Authenticator is '${authenticator}' but '${configfile}' is missing"
                return 1
            fi
            authenticator_params="--${authenticator}-credentials=${configfile}"
        fi

        if [ -n "${CERTBOT_DNS_PROPAGATION_SECONDS}" ]; then
            authenticator_params="${authenticator_params} --${authenticator}-propagation-seconds=${CERTBOT_DNS_PROPAGATION_SECONDS}"
        fi
    else
        error "Unknown authenticator '${authenticator}' for '${1}'"
        return 1
    fi

    info "Requesting an ${3^^} certificate for '${1}' (${challenge_type} through ${authenticator})"
    certbot certonly \
        --agree-tos --keep -n --text \
        --preferred-challenges ${challenge_type} \
        --authenticator ${authenticator} \
        ${authenticator_params} \
        --email "${CERTBOT_EMAIL}" \
        --server "${letsencrypt_url}" \
        --rsa-key-size "${RSA_KEY_SIZE}" \
        --elliptic-curve "${ELLIPTIC_CURVE}" \
        --key-type "${3}" \
        --cert-name "${1}" \
        ${2} \
        --debug ${force_renew}
}

# Helper function to check if a certificate is expired or not.
# $1: The domain to check
is_certificate_expired() {
    local DOMAIN=$1

    # get certificate expire date
    EXPIRE_DATE=$(echo | openssl s_client -servername $DOMAIN -connect $DOMAIN:443 2>/dev/null | openssl x509 -noout -enddate | cut -d= -f2)

    if [ -z "$EXPIRE_DATE" ]; then
        error "Error: Could not get certificate information for domain: $DOMAIN"
        STATUS=-1
    fi

    # change date to seconds
    EXPIRE_DATE_SECONDS=$(date -d "$EXPIRE_DATE" +%s)
    CURRENT_DATE_SECONDS=$(date +%s)

    # check if certificate is expired
    if [ $CURRENT_DATE_SECONDS -gt $EXPIRE_DATE_SECONDS ]; then
        STATUS=1
        info "Domain: $DOMAIN certificate expired"
    else
        STATUS=0
        info "Domain: $DOMAIN not expired yet, expires on: $EXPIRE_DATE ($EXPIRE_DATE_SECONDS)"
    fi
}

# Get all the cert names for which we should create certificate requests and
# have them signed, along with the corresponding server names.
#
# This will return an associative array that looks something like this:
# "cert_name" => "server_name1 server_name2"
declare -A certificates
for conf_file in /etc/nginx/conf.d/*.conf*; do
    parse_config_file "${conf_file}" certificates
done
nginx_reload_required=0
# Iterate over each key and make a certificate request for them.
for cert_name in "${!certificates[@]}"; do
    request_cert=0
    server_names=(${certificates["$cert_name"]})

    # Determine which type of key algorithm to use for this certificate
    # request. Having the algorithm specified in the certificate name will
    # take precedence over the environmental variable.
    if [[ "${cert_name,,}" =~ (^|[-.])ecdsa([-.]|$) ]]; then
        debug "Found variant of 'ECDSA' in name '${cert_name}"
        key_type="ecdsa"
    elif [[ "${cert_name,,}" =~ (^|[-.])ecc([-.]|$) ]]; then
        debug "Found variant of 'ECC' in name '${cert_name}"
        key_type="ecdsa"
    elif [[ "${cert_name,,}" =~ (^|[-.])rsa([-.]|$) ]]; then
        debug "Found variant of 'RSA' in name '${cert_name}"
        key_type="rsa"
    elif [ "${USE_ECDSA}" == "0" ]; then
        key_type="rsa"
    else
        key_type="ecdsa"
    fi

    # Determine the authenticator to use to solve the authentication challenge.
    # Having the authenticator specified in the certificate name will take
    # precedence over the environmental variable.
    if [[ "${cert_name,,}" =~ (^|[-.])webroot([-.]|$) ]]; then
        authenticator="webroot"
        debug "Found mention of 'webroot' in name '${cert_name}"
    elif [[ "${cert_name,,}" =~ (^|[-.])(dns-($(echo ${CERTBOT_DNS_AUTHENTICATORS} | sed 's/ /|/g')))([-.]|$) ]]; then
        authenticator=${BASH_REMATCH[2]}
        debug "Found mention of authenticator '${authenticator}' in name '${cert_name}'"
    elif [ -n "${CERTBOT_AUTHENTICATOR}" ]; then
        authenticator="${CERTBOT_AUTHENTICATOR}"
    else
        authenticator="webroot"
    fi

    # Assemble the list of domains to be included in the request from
    # the parsed 'server_names'
    domain_request=""
    check_domain=""
    for server_name in "${server_names[@]}"; do
        domain_request="${domain_request} -d ${server_name}"
        check_domain="${server_name}"
    done

    # Check if the certificate is expired or not. if check_domain is not empty and is_certificate_expired is 1, then renew the certificate
    if [ -n "${check_domain}" ]; then
        is_certificate_expired $check_domain
        if [ $STATUS -eq 0 ]; then
            info "Certifacte for domain: $check_domain is valid"
            info "Skipping certificate request for '${cert_name}'"
            STATUS=-1
            continue
            # no need to renew certificate
        elif [ $STATUS -eq 1 ]; then
            info "Renewing certificate for domain: $check_domain"
            nginx_reload_required=1
        else
            info "Check error for domain: $check_domain, request certificate"
        fi
    fi
    STATUS=-1
    # Hand over all the info required for the certificate request, and
    # let certbot decide if it is necessary to update the certificate.
    if ! get_certificate "${cert_name}" "${domain_request}" "${key_type}" "${authenticator}"; then
        error "Certbot failed for '${cert_name}'. Check the logs for details."
    fi
done

if [ $nginx_reload_required -eq 1 ]; then
    # After trying to get all our certificates, auto enable any configs that we
    # did indeed get certificates for.
    auto_enable_configs

    # Make sure the Nginx configs are valid.
    if ! nginx -t; then
    error "Nginx configuration is invalid, skipped reloading. Check the logs for details."
    exit 0
    fi

    # Finally, tell Nginx to reload the configs.
    nginx -s reload
fi

EOF


# 增加执行权限
chmod +x script/run_certbot.sh

# 生成Docker Compose文件
cat > docker-compose.yml <<EOL
version: '3'

services:
    nginx:
        image: jonasal/nginx-certbot:latest
        restart: unless-stopped
        environment:
            - CERTBOT_EMAIL=admin@qq.com
            - OPENSSL_CONF=/root/ssl/openssl.cnf
            - RENEWAL_INTERVAL=30m
        ports:
            - 80:80
            - 443:443
        volumes:
            - ./nginx_secrets:/etc/letsencrypt
            - ./user_conf.d:/etc/nginx/user_conf.d
            - ./openssl_conf:/root/ssl/
            - ./script/run_certbot.sh:/scripts/run_certbot.sh:ro
        healthcheck:
            test: ["CMD-SHELL", "curl -f http://localhost:80/ || exit 1"]
            interval: 30s
            timeout: 10s
            retries: 3
            start_period: 60s

EOL


# 启动Docker容器
echo "Starting nginx server..."
docker-compose down
docker-compose up -d