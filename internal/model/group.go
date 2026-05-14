package model

import (
	corev1 "k8s.io/api/core/v1"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
)

type GroupMember struct {
	Service *corev1.Service
	Config  sharedannotations.Config
}

type Group struct {
	Name    string
	Members []GroupMember
}
