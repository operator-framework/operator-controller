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

values = read_yaml(olmv1['yaml'])
options = values.get('options', {})
features = options.get('operatorController', {}).get('features', {})
object_controller_enabled = options.get('objectController', {}).get('enabled')
if object_controller_enabled == None:
    object_controller_enabled = options.get('operatorController', {}).get('enabled', True) and 'BoxcutterRuntime' in features.get('enabled', []) and 'BoxcutterRuntime' not in features.get('disabled', ['BoxcutterRuntime'])
if object_controller_enabled:
    olmv1['repos']['object-controller'] = {
        'image': 'quay.io/operator-framework/object-controller',
        'binary': './cmd/object-controller',
        'deployment': 'object-controller-controller-manager',
        'deps': ['api', 'cmd/object-controller', 'internal/object-controller', 'internal/shared', 'go.mod', 'go.sum'],
        'starting_debug_port': 40000,
    }

deploy_repo(olmv1, '-tags containers_image_openpgp')
