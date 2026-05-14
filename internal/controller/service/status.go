package service

import corev1 "k8s.io/api/core/v1"

func buildLoadBalancerIngress(hostname string, svc *corev1.Service) []corev1.LoadBalancerIngress {
	return []corev1.LoadBalancerIngress{
		{
			Hostname: hostname,
			Ports:    buildStatusPorts(svc),
		},
	}
}

func buildStatusPorts(svc *corev1.Service) []corev1.PortStatus {
	ports := make([]corev1.PortStatus, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		ports = append(ports, corev1.PortStatus{
			Port:     port.Port,
			Protocol: port.Protocol,
		})
	}
	return ports
}

func serviceStatusNeedsUpdate(svc *corev1.Service, hostname string) bool {
	want := buildLoadBalancerIngress(hostname, svc)
	got := svc.Status.LoadBalancer.Ingress
	if len(got) != len(want) {
		return true
	}
	if len(got) == 0 {
		return false
	}
	if got[0].Hostname != want[0].Hostname || got[0].IP != "" {
		return true
	}
	if len(got[0].Ports) != len(want[0].Ports) {
		return true
	}
	for i := range want[0].Ports {
		if got[0].Ports[i].Port != want[0].Ports[i].Port || got[0].Ports[i].Protocol != want[0].Ports[i].Protocol {
			return true
		}
	}
	return false
}
