/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/crossplane/crossplane-runtime/pkg/reconciler/oam/workload"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"

	workloadv1alpha1 "github.com/crossplane/crossplane/apis/workload/v1alpha1"
)

var (
	workloadName      = "test-workload"
	workloadNamespace = "test-namespace"
	workloadUID       = "a-very-unique-identifier"

	containerName = "test-container"
	portName      = "test-port"
)

var (
	deploymentKind       = reflect.TypeOf(appsv1.Deployment{}).Name()
	deploymentAPIVersion = appsv1.SchemeGroupVersion.String()
)

type deploymentModifier func(*appsv1.Deployment)

func dmWithContainerPorts(ports ...int32) deploymentModifier {
	return func(d *appsv1.Deployment) {
		p := []corev1.ContainerPort{}
		for _, port := range ports {
			p = append(p, corev1.ContainerPort{
				Name:          portName,
				ContainerPort: port,
			})
		}
		d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{
			Name:  containerName,
			Ports: p,
		})
	}
}

func deployment(mod ...deploymentModifier) *appsv1.Deployment {
	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       deploymentKind,
			APIVersion: deploymentAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              workloadName,
			CreationTimestamp: metav1.NewTime(time.Date(0, 0, 0, 0, 0, 0, 0, time.Local)),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					LabelKey: workloadUID,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(time.Date(0, 0, 0, 0, 0, 0, 0, time.Local)),
					Labels: map[string]string{
						LabelKey: workloadUID,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			},
		},
	}

	for _, m := range mod {
		m(d)
	}

	return d
}

type serviceModifier func(*corev1.Service)

func sWithContainerPort(target int) serviceModifier {
	return func(s *corev1.Service) {
		s.Spec.Ports = append(s.Spec.Ports, corev1.ServicePort{
			Name:       workloadName,
			Port:       int32(target),
			TargetPort: intstr.FromInt(target),
		})
	}
}

func service(mod ...serviceModifier) *corev1.Service {
	s := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       serviceKind,
			APIVersion: serviceAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: workloadName,
			Labels: map[string]string{
				LabelKey: workloadUID,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				LabelKey: workloadUID,
			},
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}

	for _, m := range mod {
		m(s)
	}

	return s
}

var _ workload.TranslationWrapper = KubeAppWrapper

func TestKubeAppWrapper(t *testing.T) {
	deployBytes, _ := json.Marshal(deployment())
	type args struct {
		w resource.Workload
		o []resource.Object
	}

	type want struct {
		result []resource.Object
		err    error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"NilObject": {
			reason: "Nil object should immediately return nil.",
			args: args{
				w: &fake.Workload{},
			},
			want: want{},
		},
		"SuccessfulWrapDeployment": {
			reason: "A Deployment should be able to be wrapped in a KubernetesApplication.",
			args: args{
				w: &fake.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: workloadNamespace,
						UID:       types.UID(workloadUID),
					},
				},
				o: []resource.Object{deployment()},
			},
			want: want{result: []resource.Object{&workloadv1alpha1.KubernetesApplication{
				ObjectMeta: metav1.ObjectMeta{
					Name: workloadName,
				},
				Spec: workloadv1alpha1.KubernetesApplicationSpec{
					ResourceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							LabelKey: workloadUID,
						},
					},
					ResourceTemplates: []workloadv1alpha1.KubernetesApplicationResourceTemplate{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:   fmt.Sprintf("%s-%s", workloadName, "deployment"),
								Labels: map[string]string{LabelKey: workloadUID},
							},
							Spec: workloadv1alpha1.KubernetesApplicationResourceSpec{
								Template: runtime.RawExtension{Raw: deployBytes},
							},
						},
					},
				},
			}},
			}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := KubeAppWrapper(context.Background(), tc.args.w, tc.args.o)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\nReason: %s\nKubeAppWrapper(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.result, r); diff != "" {
				t.Errorf("\nReason: %s\nKubeAppWrapper(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

var _ workload.TranslationWrapper = ServiceInjector

func TestServiceInjector(t *testing.T) {
	type args struct {
		w resource.Workload
		o []resource.Object
	}

	type want struct {
		result []resource.Object
		err    error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"NilObject": {
			reason: "Nil object should immediately return nil.",
			args: args{
				w: &fake.Workload{},
			},
			want: want{},
		},
		"SuccessfulInjectService_1D_1C_1P": {
			reason: "A Deployment with a port(s) should have a Service injected for first defined port.",
			args: args{
				w: &fake.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: workloadNamespace,
						UID:       types.UID(workloadUID),
					},
				},
				o: []resource.Object{deployment(dmWithContainerPorts(3000))},
			},
			want: want{result: []resource.Object{
				deployment(dmWithContainerPorts(3000)),
				service(sWithContainerPort(3000)),
			}},
		},
		"SuccessfulInjectService_1D_1C_2P": {
			reason: "A Deployment with a port(s) should have a Service injected for first defined port on the first container.",
			args: args{
				w: &fake.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: workloadNamespace,
						UID:       types.UID(workloadUID),
					},
				},
				o: []resource.Object{deployment(dmWithContainerPorts(3000, 3001))},
			},
			want: want{result: []resource.Object{
				deployment(dmWithContainerPorts(3000, 3001)),
				service(sWithContainerPort(3000)),
			}},
		},
		"SuccessfulInjectService_2D_1C_1P": {
			reason: "The first Deployment with a port(s) should have a Service injected for first defined port on the first container.",
			args: args{
				w: &fake.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: workloadNamespace,
						UID:       types.UID(workloadUID),
					},
				},
				o: []resource.Object{
					deployment(dmWithContainerPorts(4000)),
					deployment(dmWithContainerPorts(3000)),
				},
			},
			want: want{result: []resource.Object{
				deployment(dmWithContainerPorts(4000)),
				deployment(dmWithContainerPorts(3000)),
				service(sWithContainerPort(4000)),
			}},
		},
		"SuccessfulInjectService_2D_2C_2P": {
			reason: "The first Deployment with a port(s) should have a Service injected for first defined port on the first container.",
			args: args{
				w: &fake.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: workloadNamespace,
						UID:       types.UID(workloadUID),
					},
				},
				o: []resource.Object{
					deployment(dmWithContainerPorts(3000, 3001), dmWithContainerPorts(4000, 4001)),
					deployment(dmWithContainerPorts(5000, 5001), dmWithContainerPorts(6000, 6001)),
				},
			},
			want: want{result: []resource.Object{
				deployment(dmWithContainerPorts(3000, 3001), dmWithContainerPorts(4000, 4001)),
				deployment(dmWithContainerPorts(5000, 5001), dmWithContainerPorts(6000, 6001)),
				service(sWithContainerPort(3000)),
			}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := ServiceInjector(context.Background(), tc.args.w, tc.args.o)

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\nReason: %s\nServiceInjector(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.result, r); diff != "" {
				t.Errorf("\nReason: %s\nServiceInjector(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
