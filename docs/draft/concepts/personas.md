## Personas and Roles in OLM

To map the **personas** and **roles** interacting with **OLM**, the following diagrams were created. These personas represent different users who interact with OLM, either by consuming or producing content.

The personas are grouped into:
- **Consumers** – Users who consume or interact with the content managed by OLM.
- **Producers** – Users who produce content for OLM, including cluster extensions or catalogs.

## Overview of Personas:

### **Consumers:**
- **Cluster Admin** – Responsible for cluster-wide administration. May also act as:
   - **Cluster Monitor** – Focuses on platform monitoring and validation of extensions.
   - **Cluster Catalog Admin** – Manages and maintains catalogs within the cluster.
- **Cluster Extension Consumer** – Primarily interacts with and consumes content from catalogs.

### **Producers:**
- **Catalog Admin** – Oversees catalog management. May also act as:
   - **Contributor Curator** – Manages content contributions and validation.
   - **Catalog Curator** – Ensures compliance, formatting, and aggregation of contributions.
   - **Catalog Manipulator** – Handles catalog modifications, including filtering and disconnected access.
- **Extension Author** – Develops, validates, and releases cluster extensions.

The following sections provide a more detailed breakdown of each persona, including their roles and responsibilities.

## **Detailed Breakdown of Each Persona with Example Responsibilities**

### Consumers

1. **Cluster Admin**
   - May serve as:
      - Cluster Monitor
      - Cluster Catalog Admin
   - Responsibilities:
      - Scale cluster
      - Upgrade cluster
      - Miscellaneous cluster administration
      - Apply taints
      - Multi-arch tagging
      - Add/remove workers
      - Housekeeping CRDs

2. **Cluster Monitor** (Sub-role of Cluster Admin)
   - Responsibilities:
      - Platform monitoring
      - Extension monitoring
      - Notification of administrative needs

3. **Cluster Catalog Admin** (Sub-role of Cluster Admin)
   - Responsibilities:
      - Add, remove, and update catalogs
      - Manipulate pull secrets for catalog registries

4. **Extension Consumer**
   - Responsibilities:
      - Create service accounts and support infrastructure for extension lifecycle
      - Install extensions
      - Upgrade extensions
      - Remove extensions
      - View available extensions in catalog
      - Browse catalog
      - Derive minimum privilege for installation
      - Filter visibility on installable extensions
      - Observe the health of installed extensions

```mermaid
graph LR;

   %% Consumers Section
   subgraph Consumers ["Consumers"]
      CA["Cluster Admin"]
      EC["Extension Consumer"]
   end

    %% Cluster Admin Subgraph
    subgraph ClusterAdmin ["Cluster Admin"]
        CA -->|May act as| CM["Cluster Monitor"]
        CA -->|May act as| CCA["Cluster Catalog Admin"]
        CA --> CA1["Scale cluster"]
        CA --> CA2["Upgrade cluster"]
        CA --> CA3["Misc cluster administration"]
        CA --> CA4["Taint"]
        CA --> CA5["Multi-arch tagging"]
        CA --> CA6["Worker add/remove"]
        CA --> CA7["Housekeeping CRDs"]
    end

    %% Cluster Monitor Subgraph
    subgraph ClusterMonitor ["Cluster Monitor"]
        CM --> CM1["Platform monitoring"]
        CM --> CM2["Review & validate extensions"]
        CM --> CM3["Notification of administrative needs"]
    end

    %% Cluster Catalog Admin Subgraph
    subgraph ClusterCatalogAdmin ["Cluster Catalog Admin"]
        CCA --> CCA1["Adds/removes/updates catalogs"]
        CCA --> CCA2["Manipulate pull secrets to catalog registries"]
    end

    %% Styling
    classDef section fill:#EAEAEA,stroke:#000,stroke-width:1px;
    classDef graybox fill:#D3D3D3,stroke:#000,stroke-width:1px;
    classDef darkblue fill:#003366,color:#FFFFFF,stroke:#000,stroke-width:1px;
    classDef lightblue fill:#99CCFF,color:#000000,stroke:#000,stroke-width:1px;

    %% Applying Styles
    class Consumers section;
    class ClusterAdmin,ClusterMonitor,ClusterCatalogAdmin,ExtensionAdmin graybox;
    class CA,EA darkblue;
    class CM,CCA lightblue;
```

---

### Producers

1. **Catalog Admin**
   - May serve as:
      - Contributor Curator
      - Catalog Curator
      - Catalog Manipulator

2. **Contributor Curator** (Sub-role of Catalog Admin)
   - Responsibilities:
      - Validate contribution schema
      - Publish content to the registry

3. **Catalog Curator** (Sub-role of Catalog Admin)
   - Responsibilities:
      - Aggregate contributions
      - Set minimum requirements
      - Provide feedback to authors
      - Validate aggregate catalog
      - Ensure proper formatting
      - Enforce policies
      - Publish aggregate catalog

4. **Catalog Manipulator** (Sub-role of Catalog Admin)
   - Responsibilities:
      - Enable disconnected access
      - Filter catalog content

5. **Extension Author**
   - Responsibilities:
      - Create scaffold API
      - Create scaffold controller
      - Create webhook
      - Create RBAC (Role-Based Access Control)
      - Create CRDs (Custom Resource Definitions)
      - Create upgrade graph strategy
      - Build and release extensions (registry v1 example)
      - Develop app bundle
      - Develop API bundle
      - Develop operator
      - Validate extension scope
      - Validate extension upgrade graph
      - Ensure installability in test catalog
      - Adjust graph
      - Manage FBC (File-Based Catalog)
      - Apply templates
      - Publish images

```mermaid
graph LR;

   %% Producers Section
   subgraph Producers ["Producers"]
      EAU["Extension Author"]
      CA["Catalog Admin"]
   end

    %% Catalog Admin Subgraph
    subgraph CatalogAdmin ["Catalog Admin"]
        CA -->|May act as| CC["Contributor Curator"]
        CA -->|May act as| CCur["Catalog Curator"]
        CA -->|May act as| CMan["Catalog Manipulator"]
    end

    %% Extension Author Subgraph
    subgraph ExtensionAuthor ["Extension Author"]
        EAU --> EAU1["Create scaffold API"]
        EAU --> EAU2["Create scaffold controller"]
        EAU --> EAU3["Create webhook"]
        EAU --> EAU4["Create RBAC"]
        EAU --> EAU5["Create CRDs"]
        EAU --> EAU6["Create upgrade graph strategy"]
        EAU --> EAU7["Builds/releases extension (registryv1 example)"]
        EAU --> EAU8["App bundle"]
        EAU --> EAU9["API bundle"]
        EAU --> EAU10["Operator"]
        EAU --> EAU11["Validate extension scope"]
        EAU --> EAU12["Validate extension upgrade graph"]
        EAU --> EAU13["Ensure installability in test catalog"]
        EAU --> EAU14["Adjust graph"]
        EAU --> EAU15["Manage FBC (File-Based Catalog)"]
        EAU --> EAU16["Apply templates"]
        EAU --> EAU17["Publish images"]
    end

    %% Contributor Curator Subgraph
    subgraph ContributorCurator ["Contributor Curator"]
        CC --> CC1["Validate contribution schema"]
        CC --> CC2["Publish to registry"]
    end

    %% Catalog Curator Subgraph
    subgraph CatalogCurator ["Catalog Curator"]
        CCur --> CCur1["Aggregate contributions"]
        CCur --> CCur2["Set minimum requirements"]
        CCur --> CCur3["Provide author feedback"]
        CCur --> CCur4["Validate aggregate catalog"]
        CCur --> CCur5["Ensure proper formatting"]
        CCur --> CCur6["Enforce policies"]
        CCur --> CCur7["Publish aggregate catalog"]
    end

    %% Catalog Manipulator Subgraph
    subgraph CatalogManipulator ["Catalog Manipulator"]
        CMan --> CMan1["Enable disconnected access"]
        CMan --> CMan2["Filter catalog content"]
    end

    %% Styling
    classDef section fill:#EAEAEA,stroke:#000,stroke-width:1px;
    classDef graybox fill:#D3D3D3,stroke:#000,stroke-width:1px;
    classDef darkblue fill:#003366,color:#FFFFFF,stroke:#000,stroke-width:1px;
    classDef lightblue fill:#99CCFF,color:#000000,stroke:#000,stroke-width:1px;

    %% Applying Styles
    class Producers section;
    class CatalogAdmin,ExtensionAuthor,ContributorCurator,CatalogCurator,CatalogManipulator graybox;
    class CA,EAU darkblue;
    class CC,CCur,CMan lightblue;

```