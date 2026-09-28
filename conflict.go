package assembler

import "sort"

type conflictResult struct {
	items              []*basicItem
	rows, excluded     []any
	unresolved         bool
	requestContextOnly bool
}

type conflictDecision struct {
	decidedBy, winner string
	losers            []*basicItem
}

// resolveConflicts evaluates only application-declared groups, after admission.
func resolveConflicts(snapshot map[string]any, admitted []*basicItem) (conflictResult, error) {
	result := conflictResult{items: admitted, rows: []any{}, excluded: []any{}, requestContextOnly: true}
	groups := append([]any(nil), asArray(snapshot["conflicts"])...)
	sort.Slice(groups, func(i, j int) bool {
		return utf16Less(asString(asObject(groups[i])["id"]), asString(asObject(groups[j])["id"]))
	})
	byID := map[string]*basicItem{}
	for _, item := range admitted {
		byID[item.id] = item
	}
	policy := asObject(snapshot["route_policy"])
	dropped := map[string]bool{}
	for _, raw := range groups {
		group := asObject(raw)
		groupID, kind := asString(group["id"]), asString(group["kind"])
		names := append([]any(nil), asArray(group["items"])...)
		sort.Slice(names, func(i, j int) bool { return utf16Less(asString(names[i]), asString(names[j])) })
		members := []*basicItem{}
		for _, rawID := range names {
			if item := byID[asString(rawID)]; item != nil {
				item.groupID = groupID
				members = append(members, item)
			}
		}
		row := map[string]any{"group_id": groupID, "kind": kind, "items": names}
		if len(members) < 2 {
			row["decided_by"], row["resolution"] = "moot", "moot"
			result.rows = append(result.rows, row)
			continue
		}
		var decision conflictDecision
		var err error
		if kind == "instruction" {
			decision = decideInstruction(members)
		} else {
			factPolicy := objectValue(objectValue(policy["facts"])[asString(group["fact"])])
			decision, err = decideFact(members, factPolicy)
			if err != nil {
				return conflictResult{}, err
			}
		}
		protectedLoser := false
		for _, loser := range decision.losers {
			protectedLoser = protectedLoser || loser.tier == "protected"
		}
		if decision.decidedBy == "escalated" || protectedLoser {
			action := "refuse"
			if kind == "instruction" {
				if named, ok := policy["on_unresolved_instruction"].(string); ok {
					action = named
				}
			} else {
				factPolicy := objectValue(objectValue(policy["facts"])[asString(group["fact"])])
				action = asString(factPolicy["on_unresolved"])
			}
			row["decided_by"] = "escalated"
			switch action {
			case "surface":
				row["resolution"] = "surfaced"
				for _, item := range members {
					item.conflictID = groupID
				}
			case "request_context":
				row["resolution"] = "context_requested"
				result.unresolved = true
			default:
				row["resolution"] = "refused"
				result.unresolved = true
				result.requestContextOnly = false
			}
		} else {
			row["decided_by"], row["resolution"] = decision.decidedBy, "resolved"
			if decision.winner != "" {
				row["winner"] = decision.winner
			}
			for _, loser := range decision.losers {
				dropped[loser.id] = true
				reason := "conflict_lost"
				if kind == "instruction" {
					reason = "conflict_deferred"
				}
				result.excluded = append(result.excluded, map[string]any{
					"item_id": loser.id, "reason": reason, "stage": "assembler", "slot": loser.slot,
				})
			}
		}
		result.rows = append(result.rows, row)
	}
	sort.Slice(result.excluded, func(i, j int) bool {
		return utf16Less(asString(asObject(result.excluded[i])["item_id"]), asString(asObject(result.excluded[j])["item_id"]))
	})
	kept := make([]*basicItem, 0, len(admitted))
	for _, item := range admitted {
		if !dropped[item.id] {
			kept = append(kept, item)
		}
	}
	result.items = kept
	return result, nil
}

func decideInstruction(members []*basicItem) conflictDecision {
	peers := []*basicItem{}
	highest := 0
	for _, item := range members {
		rank := 0
		switch item.data["authority"] {
		case "governing":
			rank = 2
		case "user":
			rank = 1
		}
		if rank > highest {
			highest, peers = rank, []*basicItem{}
		}
		if rank > 0 && rank == highest {
			peers = append(peers, item)
		}
	}
	if len(peers) <= 1 {
		decision := conflictDecision{decidedBy: "authority"}
		if len(peers) == 1 {
			decision.winner = peers[0].id
		}
		return decision
	}
	var winner *basicItem
	for _, peer := range peers {
		switch peer.data["conflict_policy"] {
		case "governs":
			if winner != nil {
				return conflictDecision{decidedBy: "escalated"}
			}
			winner = peer
		case "defers":
		default:
			return conflictDecision{decidedBy: "escalated"}
		}
	}
	if winner == nil {
		return conflictDecision{decidedBy: "escalated"}
	}
	decision := conflictDecision{decidedBy: "policy", winner: winner.id}
	for _, peer := range peers {
		if peer != winner {
			decision.losers = append(decision.losers, peer)
		}
	}
	return decision
}

func decideFact(members []*basicItem, policy map[string]any) (conflictDecision, error) {
	bestPrecedence := len(asArray(policy["precedence"]))
	leaders := []*basicItem{}
	for _, member := range members {
		scope := objectValue(member.data["scope"])
		eligible := true
		for _, raw := range arrayValue(policy["scope"]) {
			if _, ok := scope[asString(raw)]; !ok {
				eligible = false
			}
		}
		if !eligible {
			continue
		}
		for rank, raw := range asArray(policy["precedence"]) {
			if raw != member.producerID {
				continue
			}
			if rank < bestPrecedence {
				bestPrecedence, leaders = rank, []*basicItem{}
			}
			if rank == bestPrecedence {
				leaders = append(leaders, member)
			}
			break
		}
	}
	if len(leaders) == 0 {
		return conflictDecision{decidedBy: "escalated"}, nil
	}
	winner, decidedBy := leaders[0], "policy"
	if len(leaders) > 1 {
		if policy["freshness_tiebreak"] != true {
			return conflictDecision{decidedBy: "escalated"}, nil
		}
		decidedBy = "freshness"
		unique := true
		for _, leader := range leaders[1:] {
			cmp, err := compareInstants(asString(leader.data["freshness"]), asString(winner.data["freshness"]))
			if err != nil {
				return conflictDecision{}, err
			}
			if cmp > 0 {
				winner, unique = leader, true
			} else if cmp == 0 {
				unique = false
			}
		}
		if !unique {
			return conflictDecision{decidedBy: "escalated"}, nil
		}
	}
	decision := conflictDecision{decidedBy: decidedBy, winner: winner.id}
	for _, member := range members {
		if member != winner {
			decision.losers = append(decision.losers, member)
		}
	}
	return decision, nil
}
