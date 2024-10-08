# operator-controller
The operator-controller is the central component of Operator Lifecycle Manager (OLM) v1.
It extends Kubernetes with an API through which users can install extensions.

## Overview

OLM v1 is the follow-up to [OLM v0](https://github.com/operator-framework/operator-lifecycle-manager). Its purpose is to provide APIs, 
controllers, and tooling that support the packaging, distribution, and lifecycling of Kubernetes extensions. It aims to:

- align with Kubernetes designs and user assumptions
- provide secure, high-quality, and predictable user experiences centered around declarative GitOps concepts
- give cluster admins the minimal necessary controls to build their desired cluster architectures and to have ultimate control

OLM v1 consists of two different components:

* operator-controller (this repository)
* [catalogd](https://github.com/operator-framework/catalogd)

For a more complete overview of OLM v1 and how it differs from OLM v0, see our [overview](docs/project/olmv1_design_decisions.md).

## Documentation

The documentation currently lives at [website](https://operator-framework.github.io/operator-controller/). The source of the documentation exists in this repository, see [docs directory](docs/).

## Getting Started

To get started with OLM v1, please see our [Getting Started](https://operator-framework.github.io/operator-controller/getting-started/olmv1_getting_started/) documentation.

## License

Copyright 2022-2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
