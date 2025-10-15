
# Summary

This document describes a specific use case and feedback for BindingCondition (KEP-5007): enabling dynamic device scaling.

- **What is Dynamic Device Scaling**: It refers to the mechanism of dynamically attaching and detaching resources, such as GPUs, to and from nodes in response to Pod's demands.
- **Underlying Infrastructure**: This is based on a "Composable Hardware Disaggregated Infrastructure" where resources like GPUs are managed in a resource pool and can be freely attached to or detached from nodes via an API.
- **CoHDI Project**: This development is part of the CoHDI project ([https://github.com/CoHDI](https://github.com/CoHDI)).
- **Use of BindingCondition**: We distinguish between devices in the resource pool (which have BindingCondition) and devices attached to nodes (which do not). This distinction is made because their management entities differ, meaning devices in the resource pool cannot be directly passed to DRA drivers (kubelet plugin) provided by device vendors. In this use case, instead of waiting for BindingCondition to be met, we wait for `BindingFailureCondition` to be met.

# How to Achieve Dynamic Device Scaling Using BindingCondition
1. **Visibility of Resources in the Resource Pool:**
	- We have developed a component ([https://github.com/CoHDI/composable-dra-driver](https://github.com/CoHDI/composable-dra-driver)) to output resources that are in the resource pool but not yet attached to a node into a `ResourceSlice`.
	- This `ResourceSlice` includes `BindingCondition`. While it contains information like the device's model name as an attribute, it does not carry information that uniquely identifies a specific device instance (e.g., a device UUID). It simply represents the number of devices per model.
2. **Visibility of Resources Attached to Nodes:**
	- Devices already attached to a node are assumed to be output by traditional DRA driver (kubelet plugin) provided by the device vendor and are not expected to have `BindingCondition` set.
3. **Pod Request and Scheduling:**
	- Consider a scenario where a Pod requests a GPU when resources exist in the resource pool but no resources are yet attached to a node.
	- The scheduler allocates a device from the resource pool to the Pod. Since `BindingCondition` is set at this stage, the Pod remains in a pending state, waiting for a status update from an external controller.
4. **External Controller for Device Augmentation:**
	- The external controller ([https://github.com/CoHDI/dynamic-device-scaler](https://github.com/CoHDI/dynamic-device-scaler)) monitors all `ResourceClaim`s.
	- When a `ResourceClaim` with `BindingCondition` is allocated to a Pod, the controller instructs Composable Hardware Disaggregated Infrastructure to increase the device count.
	- Once the device addition is complete and the ResourceSlice for the now-attached devices on the node is updated, the `dynamic-device-scaler` updates the `ResourceClaim` by setting `BindingFailureCondition`, thereby prompting the Pod to be rescheduled.
5. **Rescheduling and Utilization:**
	- When the Pod is rescheduled, since the device in `ResourceSlice` for the already-attached resource on the node exists, the scheduler allocates that device to the Pod. A Pod can enter the Running state and utilize the GPU.
6. **Resource Release and Return:**
	- Although unrelated to the `BindingCondition` functionality directly, if the workload completes, the external controller issues a detach instruction to remove the GPU from the node and return it to the resource pool.

# Feedback for KEP-5007

We have conducted tests on the following items and confirmed their results:

- **Realization of Basic Dynamic Device Scaling:**
    - The DRA driver (composable-dra-driver) monitoring the resource pool state can create `ResourceSlice`s with `BindingCondition`.
    - The devices described in `ResourceSlice` with `BindingCondition` can be allocated to Pods.
    - The external controller can monitor and modify the state of `ResourceClaim`s allocated to Pods. As a result of this modification (to satisfy `BindingFailureCondition`), the Pod is rescheduled. The external controller can also retrieve node name information from `ResourceClaim` if the `BindsToNode` option is set.
    - After the resource has been used, the external controller can appropriately reduce the number of resources based on the status of the `ResourceClaim`.
- **Behavior of Pending Pods:**
    - If a `ResourceSlice` with `BindingCondition` is allocated to a Pod and no notification is received from the external controller, the Pod will remain in a Pending state. If this Pending state persists for too long, the Pod will be rescheduled due to a scheduler timeout.
    - After rescheduling, and if conditions are met, the aforementioned dynamic scaling can be realized.
- **Stability:**
    - No issues such as deadlocks have been detected within the scheduler or DRA framework.
    - During testing, a few bugs were identified in the external controller (CoHDI component), but all of them were resolvable with fixes and do not indicate a fatal problem with the use of `BindingCondition`.
- **Resource Decrease Scenario Testing:**
    - Tests confirmed that when devices detected by `composable-dra-driver` decrease during a Pod's Pre-Bind phase (e.g., due to delays or other clusters), no issues arise.
    - The `dynamic-device-scaler` successfully detects such `ResourceSlice` decreases and reschedules affected Pods.
    - This works without DRA inconsistencies because the `ResourceSlice` for a resource pool shows only resource counts per model and isn't used by the on-node DRA driver.
