## pprof

This folder contains some profiles that can be read using [pprof](https://github.com/google/pprof) to show how the core kubernetes apiserver and the custom rukpak-packageserver apiserver CPU & Memory utilization is affected by the creation and reconciliation of the sample `CatalogSource` CR found at `../config/samples/rukpak_catalogsource.yaml`.

Instead of providing static screenshots and losing the interactivity associated with these `pprof` profiles, each of the files with the extension `.pb` can be used to view the profiles that were the result of running `pprof` against the live processes.

To view the `pprof` profiles in the most interactive way (or if you have no prior `pprof`experience) it is recommended to run:
```
go tool pprof -http=localhost:<port> somefile.pb
```

This will start up an interactive web UI for viewing the profile data for a specific file on `localhost:<port>`. There are quite a few different ways this data can be viewed so feel free to play around and find the view which gives you the most meaningful information.

If you know your way around `pprof` you *should* be able to run any other variations of `pprof` with these files as well.

Here is a brief breakdown of what information is provided in each profile file in this directory:
- `kubeapiserver_cpu_profile.pb` - This is the CPU utilization of the core kube-apiserver
- `kubeapiserver_heap_profile.pb` - This is the Memory utilization of the core kube-apiserver
- `rukpakapiserver_cpu_profile.pb` - This is the CPU utilization of the custom rukpak-packageserver apiserver
- `rukpakapiserver_heap_profile.pb` - This is the Memory utilization of the custom rukpak-packageserver apiserver
- `manager_cpu_profile.pb` - This is the CPU utilization of the CatalogSource controller (and other controllers associated with this manager).
- `manager_heap_profile.pb` - This is the Memory utilization of the CatalogSource controller (and other controllers associated with this manager).
