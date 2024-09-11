# Library of utility functions

debug() {
    if [ "${DEBUG,,}" != "false" ] && [ -n "$DEBUG" ]; then
        echo "DEBUG: $1" >&2
    fi
}

print-banner() {
    local red='\033[91m'
    local white='\033[97m'
    local reset='\033[0m'
    local green='\033[92m'

    echo -e "${white}===========================================================================================================================${reset}"
    echo -e "${white}‖${red}    ____                             __                  ______                                                     __   ${white}‖${reset}"
    echo -e "${white}‖${red}   / __ \ ____   ___   _____ ____ _ / /_ ____   _____   / ____/_____ ____ _ ____ ___   ___  _      __ ____   _____ / /__ ${white}‖${reset}"
    echo -e "${white}‖${red}  / / / // __ \ / _ \ / ___// __ \`// __// __ \ / ___/  / /_   / ___// __ \`// __ \`__ \ / _ \| | /| / // __ \ / ___// //_/ ${white}‖${reset}"
    echo -e "${white}‖${red} / /_/ // /_/ //  __// /   / /_/ // /_ / /_/ // /     / __/  / /   / /_/ // / / / / //  __/| |/ |/ // /_/ // /   / ,<    ${white}‖${reset}"
    echo -e "${white}‖${red} \____// .___/ \___//_/    \__,_/ \__/ \____//_/     /_/    /_/    \__,_//_/ /_/ /_/ \___/ |__/|__/ \____//_/   /_/|_|   ${white}‖${reset}"
    echo -e "${white}‖${red}      /_/${green}                                                                                                         OLM v1 ${white}‖${reset}"
    echo -e "${white}===========================================================================================================================${reset}"
}

assert-commands() {
    for cmd in "$@"; do
        if ! command -v "$cmd" &>/dev/null; then
            echo "Required command '$cmd' not found in PATH" >&2
            exit 1
        fi
    done
}

assert-container-runtime() {
    if [ -z "$CONTAINER_RUNTIME" ]; then
        if command -v podman &>/dev/null; then
                export CONTAINER_RUNTIME="podman"
            elif command -v docker &>/dev/null; then
                export CONTAINER_RUNTIME="docker"
            else
                echo "No container runtime found in PATH. If not using docker or podman, please set the CONTAINER_RUNTIME environment variable to your container runtime" >&2
                exit 1
            fi
    fi
    if ! command -v "$CONTAINER_RUNTIME" &>/dev/null; then
        echo "Configured container runtime '$CONTAINER_RUNTIME' not found in PATH" >&2
        exit 1
    fi
}