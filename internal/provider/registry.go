package provider

import (
	"fmt"
	"strings"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	models    map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		models:    make(map[string]string),
	}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
	for _, m := range p.Models() {
		key := fmt.Sprintf("%s/%s", p.Name(), m.ID)
		r.models[key] = p.Name()
	}
}

func (r *Registry) Get(providerName string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", providerName)
	}
	return p, nil
}

func (r *Registry) GetByModel(modelRef string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if providerName, ok := r.models[modelRef]; ok {
		modelID := strings.TrimPrefix(modelRef, providerName+"/")
		return r.providers[providerName], modelID, nil
	}

	parts := strings.SplitN(modelRef, "/", 2)
	if len(parts) == 2 {
		providerName := parts[0]
		modelID := parts[1]
		if p, ok := r.providers[providerName]; ok {
			if p.Supports(modelID) || len(modelID) > 0 {
				return p, modelID, nil
			}
		}
	}

	for providerName, p := range r.providers {
		for _, m := range p.Models() {
			if m.ID == modelRef {
				return p, modelRef, nil
			}
		}
		pName := providerName + "/"
		if strings.HasPrefix(modelRef, pName) {
			mID := strings.TrimPrefix(modelRef, pName)
			if mID != "" && p.Supports(mID) {
				return p, mID, nil
			}
		}
	}

	return nil, "", fmt.Errorf("no provider found for model %q", modelRef)
}

func (r *Registry) AvailableModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var models []string
	for _, p := range r.providers {
		for _, m := range p.Models() {
			models = append(models, fmt.Sprintf("%s/%s", p.Name(), m.ID))
		}
	}
	return models
}

func (r *Registry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}
