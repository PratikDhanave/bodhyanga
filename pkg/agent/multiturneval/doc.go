// Package multiturneval provides a multi-turn conversation evaluation harness
// for Bodh LLM agents.
//
// # What multi-turn evals add over single-turn
//
// Single-turn evals (lesson 04) test one isolated prompt → response pair:
//
//	Input: "What is the capital of France?"
//	Output: "Paris" ✓ or ✗
//
// Multi-turn evals (lesson 05) test whether an agent maintains coherent context
// across a scripted conversation — the kind of reasoning required in real
// clinical workflows where follow-up questions refine a diagnosis, a care plan
// evolves across patient interactions, or a user asks follow-up questions that
// depend on prior answers.
//
// A multi-turn case defines a sequence of user turns. After each turn the
// agent's response is optionally checked (intermediate CheckFn). After the
// final turn, one or more FinalEvaluators score the last response.
//
// # Key differences from single-turn
//
//  1. History accumulation: each user message and assistant reply is appended
//     to a ConversationHistory and sent back to the LLM on the next turn, so
//     the model can refer to earlier context ("that by 3" refers to "4" from
//     turn 1).
//
//  2. Intermediate checks: optional CheckFn on each ConversationTurn lets you
//     assert partial progress — e.g. "after turn 1, the agent must mention Tokyo".
//
//  3. Final scoring: FinalEvaluators on a MultiTurnCase score only the last
//     assistant response, after all turns have completed. This mirrors the
//     lmnr.evaluate() pattern where the entire conversation is the unit of
//     evaluation.
//
//  4. Structured result: MultiTurnResult records every turn's input, output,
//     and check outcome alongside the final scores, giving a full audit trail.
//
// # Usage
//
//	cases := []multiturneval.MultiTurnCase{
//	    {
//	        Name:   "Capital quiz",
//	        System: "You are a helpful geography assistant.",
//	        Turns: []multiturneval.ConversationTurn{
//	            {UserMessage: "What is the capital of Japan?"},
//	            {UserMessage: "And what about France?"},
//	        },
//	        FinalEvaluators: map[string]func(string) float64{
//	            "contains_paris": func(out string) float64 {
//	                if strings.Contains(strings.ToLower(out), "paris") {
//	                    return 1.0
//	                }
//	                return 0.0
//	            },
//	        },
//	    },
//	}
//
//	results, err := multiturneval.RunMultiTurnEval(ctx, cases, client)
//	multiturneval.PrintMultiTurnReport(results)
package multiturneval
