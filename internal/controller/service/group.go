package service

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
	sharedmodel "github.com/luomo/aws-nlb-controller/internal/model"
)

type GroupBuilder struct {
	parser *sharedannotations.Parser
}

func NewGroupBuilder(parser *sharedannotations.Parser) *GroupBuilder {
	return &GroupBuilder{parser: parser}
}

func (b *GroupBuilder) BuildForService(services []*corev1.Service, owner *corev1.Service) (sharedmodel.Group, bool, error) {
	ownerCfg, managed, err := b.parser.ParseService(owner)
	if err != nil || !managed {
		return sharedmodel.Group{}, managed, err
	}

	group := sharedmodel.Group{Name: ownerCfg.SharedNLBName}
	for _, svc := range services {
		cfg, svcManaged, err := b.parser.ParseService(svc)
		if err != nil {
			return sharedmodel.Group{}, true, err
		}
		if !svcManaged || cfg.SharedNLBName != ownerCfg.SharedNLBName {
			continue
		}
		group.Members = append(group.Members, sharedmodel.GroupMember{Service: svc, Config: cfg})
	}

	sort.Slice(group.Members, func(i, j int) bool {
		left := types.NamespacedName{Namespace: group.Members[i].Service.Namespace, Name: group.Members[i].Service.Name}
		right := types.NamespacedName{Namespace: group.Members[j].Service.Namespace, Name: group.Members[j].Service.Name}
		return left.String() < right.String()
	})

	if err := validateListenerPorts(group); err != nil {
		return sharedmodel.Group{}, true, err
	}
	if err := validateLoadBalancerSettings(group); err != nil {
		return sharedmodel.Group{}, true, err
	}
	return group, true, nil
}

func validateListenerPorts(group sharedmodel.Group) error {
	seen := map[int32]types.NamespacedName{}
	for _, member := range group.Members {
		svcKey := types.NamespacedName{Namespace: member.Service.Namespace, Name: member.Service.Name}
		for _, port := range member.Service.Spec.Ports {
			if existing, ok := seen[port.Port]; ok {
				return fmt.Errorf("shared NLB %s has duplicate listener port %d on %s and %s", group.Name, port.Port, existing, svcKey)
			}
			seen[port.Port] = svcKey
		}
	}
	return nil
}

func validateLoadBalancerSettings(group sharedmodel.Group) error {
	if len(group.Members) < 2 {
		return nil
	}
	base := group.Members[0]
	for _, member := range group.Members[1:] {
		if err := validateLoadBalancerSetting(group.Name, base, member, "scheme", base.Config.Scheme, member.Config.Scheme); err != nil {
			return err
		}
		if err := validateLoadBalancerSetting(group.Name, base, member, "ip-address-type", base.Config.IPAddressType, member.Config.IPAddressType); err != nil {
			return err
		}
		if err := validateStringMapSetting(group.Name, base, member, "tags", base.Config.Tags, member.Config.Tags); err != nil {
			return err
		}
		if err := validateStringMapSetting(group.Name, base, member, "attributes", base.Config.LoadBalancerAttributes, member.Config.LoadBalancerAttributes); err != nil {
			return err
		}
	}
	return nil
}

// validateLoadBalancerSetting reports conflict only when both sides explicitly set the value and differ.
// If one side is empty (unset), the other side's value is used — no conflict.
func validateLoadBalancerSetting(groupName string, base sharedmodel.GroupMember, member sharedmodel.GroupMember, name string, baseValue string, memberValue string) error {
	if baseValue == "" || memberValue == "" {
		return nil
	}
	if baseValue != memberValue {
		return conflictingLoadBalancerSettingError(groupName, base, member, name)
	}
	return nil
}

// validateStringMapSetting reports conflict only when both sides have entries and the maps differ.
func validateStringMapSetting(groupName string, base sharedmodel.GroupMember, member sharedmodel.GroupMember, name string, baseMap map[string]string, memberMap map[string]string) error {
	if len(baseMap) == 0 || len(memberMap) == 0 {
		return nil
	}
	if !stringMapsEqual(baseMap, memberMap) {
		return conflictingLoadBalancerSettingError(groupName, base, member, name)
	}
	return nil
}

func conflictingLoadBalancerSettingError(groupName string, base sharedmodel.GroupMember, member sharedmodel.GroupMember, name string) error {
	baseKey := types.NamespacedName{Namespace: base.Service.Namespace, Name: base.Service.Name}
	memberKey := types.NamespacedName{Namespace: member.Service.Namespace, Name: member.Service.Name}
	return fmt.Errorf("shared NLB %s has conflicting %s on %s and %s", groupName, name, baseKey, memberKey)
}

func stringMapsEqual(left map[string]string, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}
