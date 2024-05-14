if not os.path.exists('../tilt-support'):
    fail('Please clone https://github.com/operator-framework/tilt-support to ../tilt-support')

load('../tilt-support/Tiltfile', 'deploy_repo')

repo = {
    'image': 'quay.io/operator-framework/catalogd',
    'yaml': 'config/overlays/cert-manager',
    'binaries': {
        'manager': 'catalogd-controller-manager',
    },
    'starting_debug_port': 20000,
}

deploy_repo('catalogd', repo)
