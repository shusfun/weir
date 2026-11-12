package subgroup

import (
	"fmt"

	"github.com/shusfun/weir/internal/sub"
)

func validateCombination(sources []Source, manual []ManualNode, options Options) error {
	validate := func(include, exclude string, ops any) error {
		if err := sub.ValidateFilterRules(include); err != nil {
			return err
		}
		if err := sub.ValidateFilterRules(exclude); err != nil {
			return err
		}
		return sub.ValidateOperatorPatterns(ops)
	}
	if err := validate(options.FilterInclude, options.FilterExclude, options.Operators); err != nil {
		return fmt.Errorf("invalid_group_rules: %w", err)
	}
	if err := sub.ValidateOperatorPatterns(options.DefaultOperators); err != nil {
		return fmt.Errorf("invalid_default_rules: %w", err)
	}
	if err := sub.ValidateOperatorPatterns(options.LegacyTransform); err != nil {
		return fmt.Errorf("invalid_legacy_rules: %w", err)
	}
	for _, source := range sources {
		if err := validate(source.FilterInclude, source.FilterExclude, source.Operators); err != nil {
			return fmt.Errorf("invalid_source_rules[%d]: %w", source.ID, err)
		}
	}
	for _, node := range manual {
		if err := validate(node.FilterInclude, node.FilterExclude, node.Operators); err != nil {
			return fmt.Errorf("invalid_node_rules[%d]: %w", node.ID, err)
		}
	}
	return nil
}
