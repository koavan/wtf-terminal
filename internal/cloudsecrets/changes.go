package cloudsecrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// ── Changes: planned, confirmed, then made ───────────────────────────────

// Change is a new secret, or a new value for one.
type Change struct {
	// SecretsManager, ParameterStore or Vault.
	Store string `json:"store"`
	// The secret's name, the parameter's, or the Vault path (mount included).
	Name   string `json:"name"`
	Create bool   `json:"create"`
	// The new value: text, or for Vault a JSON object of its keys.
	Value string `json:"value"`
	// Parameter Store, when creating: String, StringList or SecureString.
	Type        string `json:"type"`
	Description string `json:"description"`
	// The version the user saw (Value.Version); "" when creating.
	Version string `json:"version"`
	// Vault: the mount's KV version (1 or 2).
	KV int `json:"kv"`
}

var paramTypes = []string{"String", "StringList", "SecureString"}

func (c Change) check() error {
	switch c.Store {
	case SecretsManager:
		if err := validName("secret", c.Name); err != nil {
			return err
		}
	case ParameterStore:
		if err := validName("parameter", c.Name); err != nil {
			return err
		}
		if c.Create && !slices.Contains(paramTypes, c.Type) {
			return fmt.Errorf("%q isn't a parameter type", c.Type)
		}
	case Vault:
		if err := validPath(c.Name); err != nil {
			return err
		}
		if c.KV != 1 && c.KV != 2 {
			return errors.New("unknown KV version")
		}
		if _, err := vaultData(c.Value); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown store %q", c.Store)
	}
	if c.Value == "" {
		return errors.New("the value is empty")
	}
	if !utf8.ValidString(c.Value) {
		return errors.New("the value isn't text")
	}
	return nil
}

// vaultData reads a Vault value: a JSON object.
func vaultData(v string) (map[string]any, error) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(v), &obj); err != nil || obj == nil {
		return nil, errors.New(`a Vault secret is a JSON object of keys and values, like {"password": "…"}`)
	}
	if len(obj) == 0 {
		return nil, errors.New("the secret has no keys")
	}
	return obj, nil
}

// state is what's there now: whether it exists, its version and value.
type state struct {
	exists  bool
	version string
	text    string
	param   parameter
	where   string
}

func (s *Service) current(ctx context.Context, r Request, c Change) (state, error) {
	var st state
	switch c.Store {
	case SecretsManager, ParameterStore:
		t := s.Target(r)
		if t.AWSError != "" {
			return st, errors.New(t.AWSError)
		}
		st.where = fmt.Sprintf("Account %s · profile %s · %s", orUnknown(t.Account), t.Profile, t.Region)
	case Vault:
		addr := r.Env["VAULT_ADDR"]
		if addr == "" {
			addr = "the vault CLI's default server"
		}
		st.where = "Vault " + addr
		if ns := r.Env["VAULT_NAMESPACE"]; ns != "" {
			st.where += " · namespace " + ns
		}
	}
	switch c.Store {
	case SecretsManager:
		v, err := s.ReadSecret(r, c.Name)
		if isAWSNotFound(err) {
			return st, nil
		}
		if err != nil {
			return st, err
		}
		st.exists, st.version, st.text = true, v.Version, v.Text
	case ParameterStore:
		ctx2, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		out, _, err := s.aws(ctx2, r, nil, "ssm", "describe-parameters", "--parameter-filters", "Key=Name,Option=Equals,Values="+c.Name)
		if err != nil {
			return st, err
		}
		var res struct{ Parameters []parameter }
		if json.Unmarshal(out, &res) != nil {
			return st, errors.New("unexpected answer from AWS (describe-parameters)")
		}
		for _, p := range res.Parameters {
			if p.Name == c.Name {
				st.exists, st.param, st.version = true, p, fmt.Sprint(p.Version)
			}
		}
		if st.exists && !c.Create {
			v, err := s.ReadParameter(r, c.Name)
			if err != nil {
				return st, err
			}
			st.text = v.Text
		}
	case Vault:
		v, err := s.VaultRead(r, c.Name, c.KV)
		if notFound(err) {
			return st, nil
		}
		if err != nil {
			return st, err
		}
		st.exists, st.version, st.text = true, v.Version, v.Text
	}
	return st, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func isAWSNotFound(err error) bool {
	var ce *cliError
	return errors.As(err, &ce) && (strings.Contains(ce.stderr, "ResourceNotFoundException") || strings.Contains(ce.stderr, "ParameterNotFound"))
}

// verify checks the change still fits what's there, and says what it does.
func (s *Service) verify(ctx context.Context, r Request, c Change) (state, []string, error) {
	if err := c.check(); err != nil {
		return state{}, nil, err
	}
	st, err := s.current(ctx, r, c)
	if err != nil {
		return st, nil, err
	}
	kind := map[string]string{SecretsManager: "secret", ParameterStore: "parameter", Vault: "Vault secret"}[c.Store]
	if c.Create {
		if st.exists {
			return st, nil, fmt.Errorf("a %s named %s already exists", kind, c.Name)
		}
		var steps []string
		switch c.Store {
		case SecretsManager:
			steps = append(steps, fmt.Sprintf("Create secret %s, encrypted with the account's default key (aws/secretsmanager)", c.Name))
		case ParameterStore:
			what := c.Type
			if c.Type == "SecureString" {
				what += ", encrypted with the account's default key (aws/ssm)"
			}
			steps = append(steps, fmt.Sprintf("Create parameter %s (%s)", c.Name, what))
		case Vault:
			steps = append(steps, "Create "+c.Name)
		}
		steps = append(steps, st.where, describeValue("", c.Value, c.Store == Vault))
		if c.Description != "" && c.Store != Vault {
			steps = append(steps, "Description: "+c.Description)
		}
		return st, steps, nil
	}
	if !st.exists {
		return st, nil, fmt.Errorf("the %s %s isn't there any more", kind, c.Name)
	}
	if c.Store != Vault || c.KV == 2 {
		if st.version != c.Version {
			return st, nil, fmt.Errorf("%s changed since you opened it (now version %s); open it again to see the new value", c.Name, st.version)
		}
	}
	diff := describeValue(st.text, c.Value, c.Store == Vault)
	if diff == "" {
		return st, nil, errors.New("nothing to change: the value is the same")
	}
	var steps []string
	switch c.Store {
	case SecretsManager:
		steps = append(steps, fmt.Sprintf("Store a new version of secret %s; it becomes AWSCURRENT and the one before stays as AWSPREVIOUS", c.Name))
	case ParameterStore:
		what := st.param.Type
		if st.param.Type == "SecureString" {
			what += ", same KMS key " + orDefault(st.param.KeyId, "alias/aws/ssm")
		}
		steps = append(steps, fmt.Sprintf("Overwrite parameter %s: version %s → %d (%s)", c.Name, st.version, st.param.Version+1, what))
	case Vault:
		if c.KV == 2 {
			n := 0
			fmt.Sscan(st.version, &n)
			steps = append(steps, fmt.Sprintf("Write version %d of %s (version %s stays in its history)", n+1, c.Name, st.version))
		} else {
			steps = append(steps, fmt.Sprintf("Replace %s. This is a KV version 1 mount: it keeps no history, so the old value is gone", c.Name))
		}
	}
	steps = append(steps, st.where, diff, "Anything that reads it gets the new value from its next read")
	return st, steps, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// describeValue says how a value changes without showing any of it:
// which keys of a JSON object change, or its length.
func describeValue(old, new string, vault bool) string {
	oldObj, newObj := jsonObject(old), jsonObject(new)
	if newObj != nil && (oldObj != nil || old == "") {
		var changed, added, removed []string
		for k, v := range newObj {
			o, had := oldObj[k]
			switch {
			case !had:
				added = append(added, k)
			case string(o) != string(v):
				changed = append(changed, k)
			}
		}
		for k := range oldObj {
			if _, ok := newObj[k]; !ok {
				removed = append(removed, k)
			}
		}
		if old == "" {
			return fmt.Sprintf("Keys: %s (values not shown)", list(added))
		}
		var parts []string
		if len(changed) > 0 {
			parts = append(parts, "changes "+list(changed))
		}
		if len(added) > 0 {
			parts = append(parts, "adds "+list(added))
		}
		if len(removed) > 0 {
			parts = append(parts, "removes "+list(removed))
		}
		if len(parts) == 0 {
			return ""
		}
		return "Keys: " + strings.Join(parts, " · ") + " (values not shown)"
	}
	if old == new {
		return ""
	}
	n := utf8.RuneCountInString(new)
	if old == "" {
		return fmt.Sprintf("Value: %d characters (not shown)", n)
	}
	return fmt.Sprintf("Value: %d → %d characters (not shown)", utf8.RuneCountInString(old), n)
}

// jsonObject is a JSON object's members, normalized; nil if it isn't one.
func jsonObject(s string) map[string]string {
	var obj map[string]json.RawMessage
	if !strings.HasPrefix(strings.TrimSpace(s), "{") || json.Unmarshal([]byte(s), &obj) != nil {
		return nil
	}
	out := map[string]string{}
	for k, raw := range obj {
		var v any
		json.Unmarshal(raw, &v)
		b, _ := json.Marshal(v)
		out[k] = string(b)
	}
	return out
}

func list(keys []string) string {
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// PlanChange checks a change can be made and lists what it will do. No
// value appears in the plan.
func (s *Service) PlanChange(r Request, c Change) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*timeout)
	defer cancel()
	_, steps, err := s.verify(ctx, r, c)
	return steps, err
}

// ApplyChange makes a change the user confirmed, after checking again that
// nothing changed since they saw the plan.
func (s *Service) ApplyChange(r Request, c Change) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*timeout)
	defer cancel()
	st, _, err := s.verify(ctx, r, c)
	if err != nil {
		return err
	}
	input := func(v any) []byte { b, _ := json.Marshal(v); return b }
	const fromStdin = "file:///dev/stdin"
	switch c.Store {
	case SecretsManager:
		if c.Create {
			in := map[string]any{"Name": c.Name, "SecretString": c.Value}
			if c.Description != "" {
				in["Description"] = c.Description
			}
			_, _, err = s.aws(ctx, r, input(in), "secretsmanager", "create-secret", "--cli-input-json", fromStdin)
		} else {
			_, _, err = s.aws(ctx, r, input(map[string]any{"SecretId": c.Name, "SecretString": c.Value}), "secretsmanager", "put-secret-value", "--cli-input-json", fromStdin)
		}
	case ParameterStore:
		in := map[string]any{"Name": c.Name, "Value": c.Value}
		if c.Create {
			in["Type"] = c.Type
			if c.Description != "" {
				in["Description"] = c.Description
			}
		} else {
			p := st.param
			in["Type"], in["Overwrite"] = p.Type, true
			if p.Type == "SecureString" && p.KeyId != "" {
				in["KeyId"] = p.KeyId
			}
			if p.Tier != "" {
				in["Tier"] = p.Tier
			}
			if p.DataType != "" && p.DataType != "text" {
				in["DataType"] = p.DataType
			}
		}
		_, _, err = s.aws(ctx, r, input(in), "ssm", "put-parameter", "--cli-input-json", fromStdin)
	case Vault:
		data, _ := vaultData(c.Value)
		args := []string{"kv", "put", "-format=json"}
		if c.KV == 2 {
			cas := "0"
			if !c.Create {
				cas = st.version
			}
			args = append(args, "-cas="+cas)
		}
		_, err = s.vault(ctx, r, input(data), append(args, c.Name, "-")...)
	}
	return err
}
