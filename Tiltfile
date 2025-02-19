load('.tilt-support', 'deploy_repo')

operator_controller = {
    'image': 'quay.io/operator-framework/operator-controller',
    'yaml': 'config/overlays/tilt-local-dev/operator-controller',
    'binaries': {
        './cmd/operator-controller': 'operator-controller-controller-manager',
    },
    'deps': ['api', 'cmd/operator-controller', 'internal', 'pkg', 'go.mod', 'go.sum'],
    'starting_debug_port': 30000,
}
deploy_repo('operator-controller', operator_controller, '-tags containers_image_openpgp')

catalogd = {
    'image': 'quay.io/operator-framework/catalogd',
    'yaml': 'config/overlays/tilt-local-dev/catalogd',
    'binaries': {
        './catalogd/cmd/catalogd': 'catalogd-controller-manager',
    },
    'deps': ['catalogd/api', 'catalogd/cmd/catalogd', 'catalogd/internal', 'catalogd/pkg', 'go.mod', 'go.sum'],
    'starting_debug_port': 20000,
}

deploy_repo('catalogd', catalogd, '-tags containers_image_openpgp')
