load('.tilt-support', 'deploy_repo')

olmv1 = {
    'repos': {
        'catalogd': {
            'image': 'quay.io/operator-framework/catalogd',
            'binary': './cmd/catalogd',
            'deployment': 'catalogd-controller-manager',
            'deps': ['api', 'cmd/catalogd', 'internal/catalogd', 'internal/shared', 'go.mod', 'go.sum'],
            'starting_debug_port': 20000,
        },
        'operator-controller': {
            'image': 'quay.io/operator-framework/operator-controller',
            'binary': './cmd/operator-controller',
            'deployment': 'operator-controller-controller-manager',
            'deps': ['api', 'cmd/operator-controller', 'internal/operator-controller', 'internal/shared', 'go.mod', 'go.sum'],
            'starting_debug_port': 30000,
        },
    },
    'yaml': 'helm/tilt.yaml',
}

deploy_repo(olmv1, '-tags containers_image_openpgp')
