package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// SecretValue decodes either a plain YAML scalar or a resolver mapping:
//   {from_env: VAR, default: ...}
//   {from_file: path}
//   {from_k8s_secret: ns/name#key, default: ...}
type SecretValue struct {
	Raw           string
	FromEnv       string
	FromFile      string
	FromK8sSecret string
	Default       *SecretValue
	HasValue      bool
}

func (sv *SecretValue) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		sv.Raw = value.Value
		sv.HasValue = true
		return nil
	}
	if value.Kind == yaml.MappingNode {
		var m struct {
			FromEnv       string       `yaml:"from_env"`
			FromFile      string       `yaml:"from_file"`
			FromK8sSecret string       `yaml:"from_k8s_secret"`
			Default       *SecretValue `yaml:"default"`
		}
		if err := value.Decode(&m); err != nil {
			return err
		}
		sv.FromEnv = m.FromEnv
		sv.FromFile = m.FromFile
		sv.FromK8sSecret = m.FromK8sSecret
		sv.Default = m.Default
		sv.HasValue = true
		return nil
	}
	return fmt.Errorf("unexpected YAML node kind %d for secret value", value.Kind)
}

type K8sResolver struct {
	mu     sync.Mutex
	client kubernetes.Interface
	err    error
	tried  bool
}

func (kr *K8sResolver) getClient(kubeconfig string) (kubernetes.Interface, error) {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	if kr.tried {
		return kr.client, kr.err
	}
	kr.tried = true

	var cfg *rest.Config
	var err error
	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			if kc := os.Getenv("KUBECONFIG"); kc != "" {
				cfg, err = clientcmd.BuildConfigFromFlags("", kc)
			}
		}
	}
	if err != nil {
		kr.err = err
		return nil, err
	}
	c, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		kr.err = err
		return nil, err
	}
	kr.client = c
	return c, nil
}

func (sv *SecretValue) Resolve(ctx context.Context, baseDir string, k8s *K8sResolver, kubeconfig string) (string, error) {
	if sv == nil || !sv.HasValue {
		return "", nil
	}
	if sv.FromEnv != "" {
		if val, ok := os.LookupEnv(sv.FromEnv); ok && val != "" {
			return val, nil
		}
		if sv.Default != nil {
			return sv.Default.Resolve(ctx, baseDir, k8s, kubeconfig)
		}
		return "", fmt.Errorf("environment variable %q is not set", sv.FromEnv)
	}
	if sv.FromFile != "" {
		p := sv.FromFile
		if !filepath.IsAbs(p) && baseDir != "" {
			p = filepath.Join(baseDir, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			if sv.Default != nil {
				return sv.Default.Resolve(ctx, baseDir, k8s, kubeconfig)
			}
			return "", fmt.Errorf("read secret file %q: %w", p, err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	if sv.FromK8sSecret != "" {
		val, err := resolveK8sSecret(ctx, sv.FromK8sSecret, k8s, kubeconfig)
		if err == nil && val != "" {
			return val, nil
		}
		if sv.Default != nil {
			return sv.Default.Resolve(ctx, baseDir, k8s, kubeconfig)
		}
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("k8s secret %q returned empty value", sv.FromK8sSecret)
	}
	return sv.Raw, nil
}

func (sv *SecretValue) ResolveBool(ctx context.Context, baseDir string, k8s *K8sResolver, kubeconfig string, fallback bool) bool {
	if sv == nil || !sv.HasValue {
		return fallback
	}
	s, err := sv.Resolve(ctx, baseDir, k8s, kubeconfig)
	if err != nil || s == "" {
		return fallback
	}
	b, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return b
}

func resolveK8sSecret(ctx context.Context, ref string, k8s *K8sResolver, kubeconfig string) (string, error) {
	// Format: NAMESPACE/NAME#KEY
	nsAndName, key, ok := strings.Cut(ref, "#")
	if !ok || key == "" {
		return "", fmt.Errorf("invalid k8s secret ref %q (expected NAMESPACE/NAME#KEY)", ref)
	}
	ns, name, ok := strings.Cut(nsAndName, "/")
	if !ok || ns == "" || name == "" {
		return "", fmt.Errorf("invalid k8s secret ref %q (expected NAMESPACE/NAME#KEY)", ref)
	}
	client, err := k8s.getClient(kubeconfig)
	if err != nil {
		return "", fmt.Errorf("k8s client for %q: %w", ref, err)
	}
	sec, err := client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get k8s secret %s/%s: %w", ns, name, err)
	}
	raw, exists := sec.Data[key]
	if !exists {
		return "", fmt.Errorf("key %q missing in secret %s/%s", key, ns, name)
	}
	return strings.TrimRight(string(raw), "\r\n"), nil
}
