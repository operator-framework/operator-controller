load('.tilt-support', 'deploy_repo', 'process_yaml')

operator_controller = {
    'image': 'quay.io/operator-framework/operator-controller',
    'binaries': {
        './cmd/operator-controller': 'operator-controller-controller-manager',
    },
    'deps': ['api', 'cmd/operator-controller', 'internal/operator-controller', 'internal/shared', 'go.mod', 'go.sum'],
    'starting_debug_port': 30000,
}
deploy_repo('operator-controller', operator_controller, '-tags containers_image_openpgp')

catalogd = {
    'image': 'quay.io/operator-framework/catalogd',
    'binaries': {
        './cmd/catalogd': 'catalogd-controller-manager',
    },
    'deps': ['api', 'cmd/catalogd', 'internal/catalogd', 'internal/shared', 'go.mod', 'go.sum'],
    'starting_debug_port': 20000,
}

deploy_repo('catalogd', catalogd, '-tags containers_image_openpgp')
process_yaml(read_file('dev-manifests/operator-controller-tilt.yaml'))
