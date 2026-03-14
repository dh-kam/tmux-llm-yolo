package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed messages.json
var embeddedMessages []byte

var localeMessages = make(map[string]map[string]string)

const (
	DefaultLocale    = "en"
	DefaultAppLocale = "ko"
)

var localeAliases = map[string]string{
	"en":    "en",
	"en-us": "en",
	"en-gb": "en",
	"ko":    "ko",
	"ko-kr": "ko",
	"ja":    "ja",
	"jp":    "ja",
	"zh":    "zh",
	"zh-cn": "zh",
	"zh-tw": "zh",
	"vi":    "vi",
	"hi":    "hi",
	"ru":    "ru",
	"es":    "es",
	"fr":    "fr",
}

var translations = map[string]map[string]string{
	"en": englishStrings,
	"ko": koreanStrings,
	"ja": englishStrings,
	"zh": englishStrings,
	"vi": englishStrings,
	"hi": englishStrings,
	"ru": englishStrings,
	"es": englishStrings,
	"fr": englishStrings,
}

var englishStrings = map[string]string{
	"app.locale.name":                              "English",
	"app.default_continue_message":                 "continue. Carry on until verification is complete and proceed with remaining work.",
	"cmd.default_continue_message":                 "continue. Carry on until verification is complete and proceed with remaining work.",
	"cmd.watch_start":                              "watch start: session=%s interval=%ds duration=%ds",
	"cmd.watch_llm_status":                         "llm=%s model=%s fallback_llm=%s fallback_model=%s policy=%s capture=%d suspect_waits=%ds/%ds/%ds",
	"cmd.check_llm_status":                         "llm=%s model=%s fallback_llm=%s fallback_model=%s",
	"cmd.check_provider_selected":                  "LLM provider selected: %s",
	"cmd.watch_dryrun":                             "dry-run=%t format=%s",
	"cmd.watch_auto_update_flag":                   "auto-update=%t",
	"cmd.watch_auto_update_error":                  "auto-update error: %v",
	"cmd.watch_tmux_target":                        "tmux target: %s",
	"cmd.watch_current_sessions":                   "tmux sessions:",
	"cmd.watch_session_item":                       "  - %s",
	"cmd.watch_end":                                "watch end",
	"cmd.watch_auto_update_applied":                "auto-update applied: %s -> %s",
	"cmd.watch_auto_update_restart":                "restart self-updating process",
	"cmd.check_start":                              "check start: session=%s capture=%d",
	"cmd.check_offline_mode":                       "offline check mode: capture file=%s",
	"cmd.check_offline_empty":                      "offline check complete: empty capture",
	"cmd.check_result":                             "check result: %s / %s",
	"cmd.check_session_not_found":                  "result: tmux session not found",
	"cmd.watchstate_default":                       "watch state",
	"cmd.error_interval":                           "interval-seconds must be greater than 0",
	"cmd.error_suspect_wait_1":                     "suspect-wait-seconds-1 must be greater than 0",
	"cmd.error_suspect_wait_2":                     "suspect-wait-seconds-2 must be greater than 0",
	"cmd.error_suspect_wait_3":                     "suspect-wait-seconds-3 must be greater than 0",
	"cmd.error_duration":                           "duration-seconds must be greater than 0",
	"cmd.error_capture_lines":                      "capture-lines must be greater than 0",
	"cmd.error_watch_state_dir":                    "watch state dir creation failed: %v",
	"cmd.error_tmux_session_list":                  "tmux session list failed: %v",
	"cmd.error_capture_file_read":                  "failed to read capture file (%s): %v",
	"cmd.error_tmux_capture_failed":                "tmux pane capture failed (session=%s): %v",
	"cmd.warn_tmux_session_list_failed":            "warning: tmux session list failed: %v",
	"cmd.warn_tmux_session_fallback":               "warning: tmux session fallback check failed: %v",
	"cmd.warn_session_state_prompt_save":           "warning: failed to save session-state prompt (%s): %s",
	"cmd.watch_session_state_request":              "session-state request: binary=%s prompt_len=%d",
	"cmd.watch_session_state_response_fail":        "session-state response failed (%v) elapsed=%s",
	"cmd.watch_session_state_response_ok":          "session-state response succeeded (elapsed=%s)",
	"cmd.warn_session_state_result_save":           "warning: failed to save session-state result (%s): %s",
	"cmd.warn_session_state_parse_failed":          "warning: session-state parse failed: %s",
	"cmd.warn_classify_prompt_save":                "warning: failed to save check prompt (%s): %s",
	"cmd.warn_classify_result_save":                "warning: failed to save check decision output (%s): %s",
	"cmd.reason_capture_read_failed":               "capture read failed",
	"cmd.reason_empty_capture":                     "captured output is empty; skipping decision",
	"cmd.reason_completed_fixture":                 "capture file name indicates completed fixture: %s",
	"cmd.reason_progress_default_evidence":         "inferred from prompt area context",
	"cmd.reason_working":                           "working: %s",
	"cmd.reason_llm_exec_failed":                   "llm execution failed: %s",
	"cmd.reason_no_reason":                         "(no reason provided)",
	"cmd.classify_request":                         "classification request: provider=%s binary=%s prompt_len=%d",
	"cmd.classify_response_fail":                   "classification failed (%v) elapsed=%s",
	"cmd.classify_response_ok":                     "classification succeeded (elapsed=%s)",
	"watch.reason_completion_ready_transition":     "completion-ready signal detected; force WORKING->COMPLETED",
	"cmd.log_llm_usage":                            "llm usage(%s): source=%s, remaining=%d",
	"cmd.log_llm_usage_unknown":                    "llm usage(%s): provider did not report usage (source=%s)",
	"cmd.log_llm_selected":                         "llm(%s): %s (%s)",
	"cmd.error_no_llm_provider":                    "no usable llm provider available",
	"cmd.error_llm_provider_init_failed":           "llm provider initialization failed: %s",
	"cmd.warn_capture_artifacts_dir":               "warning: failed to create captured directory (%s): %s",
	"cmd.warn_capture_raw_write":                   "warning: failed to save captured ansi (%s): %s",
	"cmd.warn_capture_plain_write":                 "warning: failed to save captured plain (%s): %s",
	"cmd.warn_capture_decision_marshal":            "warning: failed to marshal decision json: %s",
	"cmd.warn_capture_decision_json_write":         "warning: failed to save decision json (%s): %s",
	"cmd.warn_state_file_dir_mkdir":                "warning: failed to create state file directory (%s): %s",
	"cmd.warn_capture_text_write":                  "warning: failed to save capture result (%s): %s",
	"watch.task_check_deadline_description":        "check watch deadline exceeded condition",
	"watch.task_base_capture_description":          "perform dual base capture",
	"watch.task_watch_capture_description":         "perform watch-cycle base capture",
	"watch.task_ansi_recheck_description":          "%dth waiting-suspicion ANSI recheck",
	"watch.task_interpret_force_input_description": "interpret long-stable ANSI as forced input target",
	"watch.task_interpret_interactive_description": "prioritize interactive prompt and allow ANSI movement",
	"watch.task_interpret_default_description":     "interpret prompt position and last output block",
	"watch.state_once_interpret":                   "once mode: immediately interpret current screen",
	"watch.state_once_no_assistant":                "once mode: no assistant UI signature, exit without sending input",
	"watch.state_once_copilot_clear":               "once mode: clear Copilot slash command state and exit",
	"watch.state_once_processing":                  "once mode: processing signal detected, exit without sending input",
	"watch.state_once_replace_copilot":             "once mode: replace pending copilot slash-start text and send continue",
	"watch.state_once_submit_pending":              "once mode: pending input exists, execute submit-only and exit",
	"watch.state_once_plan":                        "once mode: apply deterministic input plan",
	"watch.state_once_llm_fail":                    "once mode: LLM interpretation failed, send default continue",
	"watch.state_once_unknown":                     "once mode: unknown waiting state, send default continue",
	"watch.state_watch_monitoring":                 "watching",
	"watch.state_monitoring":                       "monitoring",
	"watch.reason_bypass_prompt_wait":              "interactive prompt detected while monitoring suspicion; bypass ANSI stability checks and start interpretation",
	"watch.reason_capture_capture_init":            "first base ANSI capture saved, wait %s for next periodic monitoring",
	"watch.reason_watch_deadline_exceeded":         "watch deadline exceeded",
	"watch.reason_capture_changed":                 "base capture changed from previous, return to monitoring",
	"watch.reason_capture_changed_wait":            "capture changed, return to monitoring in %s",
	"watch.reason_stable_promote":                  "ANSI unchanged for >= %s, promote to confident waiting",
	"watch.reason_suspect_initial":                 "ANSI identical to base capture for %s; waiting suspicion 1",
	"watch.reason_suspect_wait":                    "%s-th waiting-suspicion stage: recheck after %s",
	"watch.reason_min_stable_time":                 "ANSI unchanged, extend suspicion to satisfy minimum stable time %s and recheck with stage %d",
	"watch.reason_confident_by_repetition":         "confident waiting: 5 unchanged captures across 20s",
	"watch.interpret_force_input_description":      "treat long-stable ANSI as forced input when deciding action",
	"watch.interpret_interactive_description":      "prioritize interactive prompt and allow ANSI variance",
	"watch.interpret_default_description":          "interpret latest output block and decide action",
	"watch.interpret_start":                        "begin waiting-screen interpretation",
	"watch.interpret_prompt_changed":               "screen changed before interpretation; return to monitoring",
	"watch.interpret_prompt_changed_wait":          "screen changed before interpretation; wait %s and capture again",
	"watch.interpret_assistant_signature_weak":     "assistant UI signature weak; force continue on stable ANSI",
	"watch.interpret_no_assistant_monitor":         "assistant UI signature missing; no input and continue monitoring after %s",
	"watch.interpret_copilot_clear":                "clear Copilot slash state and return to monitoring",
	"watch.interpret_processing_force_continue":    "stable ANSI looks processing-like; force continue",
	"watch.interpret_processing_wait":              "processing signal present; continue monitoring after %s",
	"watch.interpret_replace_pending":              "replace pending copilot slash-start text with default continue",
	"watch.interpret_submit_pending":               "pending input exists; execute submit-only",
	"watch.interpret_plan":                         "apply deterministic plan",
	"watch.interpret_llm_fail":                     "LLM interpretation failed, send default continue",
	"watch.interpret_unknown":                      "unknown waiting state, send default continue",
	"watch.interpret_force_input_override":         "forced continue due unchanged ANSI while LLM returned SKIP",
	"watch.sleep_capture_init":                     "first base capture saved; wait %s before next monitoring capture",
	"watch.sleep_capture_changed":                  "capture changed; wait %s and continue monitoring",
	"watch.sleep_interpret_prompt_changed":         "interpretation pre-check capture changed; wait %s and recapture",
	"watch.sleep_no_assistant_monitor":             "assistant UI missing; wait %s and continue monitoring",
	"watch.sleep_processing_wait":                  "processing signal detected; wait %s and continue monitoring",
	"watch.sleep_after_send":                       "after sending input, wait before next capture in %s",
	"watch.sleep_after_choice":                     "after choice input, wait before next capture in %s",
	"watch.sleep_after_cursor":                     "after cursor selection, wait before next capture in %s",
	"watch.sleep_after_submit":                     "after submit-only, wait before next capture in %s",
	"watch.sleep_after_clear":                      "after clear-prompt, wait before next capture in %s",
	"watch.sleep_after_input_value":                "after input value send, wait before next capture in %s",
	"watch.once_after_input":                       "once mode: continue sent, stop",
	"watch.once_after_choice":                      "once mode: choice sent, stop",
	"watch.once_after_cursor":                      "once mode: cursor-confirm done, stop",
	"watch.once_after_input_submit":                "once mode: input sent, stop",
	"watch.once_after_submit":                      "once mode: submit-only executed, stop",
	"watch.once_after_clear":                       "once mode: prompt cleared, stop",
	"watch.fallback_empty_input":                   "empty planned input was returned; fallback to default continue",
	"watch.fallback_skip":                          "%s returned SKIP; fallback to continue",
	"watch.fallback_skip_watch":                    "LLM returned SKIP; fallback to continue",
	"watch.fallback_cursor":                        "%s cursor probe/selection failed; fallback to continue",
	"watch.plan_remaining_items":                   "Continue from item %s: %s, verify before moving to next.",
	"watch.waiting_reason_force_input":             "fallback forced continue after stable ANSI",
	"executor.intent_continue":                     "send free text input as continue message",
	"executor.intent_choice":                       "select a numbered choice and submit",
	"executor.intent_cursor":                       "move cursor and submit selected item",
	"executor.intent_input":                        "send free text input",
	"executor.intent_submit_pending":               "submit existing pending input",
	"executor.intent_clear_prompt":                 "clear prompt state",
	"executor.cursor_note_probe":                   "cursor probe: down then up to verify first row",
	"executor.cursor_move":                         "cursor move: down %d",
	"executor.plan_output_action":                  "action=%s",
	"executor.plan_output_intent":                  "intent=%s",
	"ui.value_none":                                "-",
	"ui.card_title_session":                        "Session",
	"ui.card_title_flow":                           "Flow",
	"ui.card_title_control":                        "Control",
	"ui.card_title_timing":                         "Timing",
	"ui.card_title_ai":                             "AI",
	"ui.card_title_detail":                         "Detail",
	"ui.label_target":                              "target",
	"ui.label_mode":                                "mode",
	"ui.label_capture":                             "capture",
	"ui.label_state":                               "state",
	"ui.label_now":                                 "now",
	"ui.label_queued":                              "queued",
	"ui.label_next":                                "next",
	"ui.label_wait":                                "wait",
	"ui.label_sleep":                               "sleep",
	"ui.label_expire":                              "expire",
	"ui.label_policy":                              "policy",
	"ui.label_llm":                                 "llm",
	"ui.label_continue":                            "cont",
	"ui.label_doing":                               "doing",
	"ui.label_event":                               "event",
	"ui.label_next_action":                         "next",
	"ui.log_panel_title":                           "Activity Log",
	"ui.sleep_status":                              "%s / %s (%s left)",
	"ui.updated_label":                             "updated",
	"ui.just_now":                                  "just now",
	"ui.ago":                                       "%s (%s ago)",
	"ui.compact_state_monitoring":                  "monitor",
	"ui.compact_state_suspect_waiting_stage_1":     "wait-1",
	"ui.compact_state_suspect_waiting_stage_2":     "wait-2",
	"ui.compact_state_suspect_waiting_stage_3":     "wait-3",
	"ui.compact_state_confident_waiting":           "waiting",
	"ui.compact_state_interpreting":                "interpreting",
	"ui.compact_state_acting":                      "acting",
	"ui.compact_state_completed":                   "done",
	"ui.compact_state_stopped":                     "stopped",
	"ui.compact_task_capture":                      "capture",
	"ui.compact_task_compare_capture":              "compare",
	"ui.compact_task_interpret":                    "interpret",
	"ui.compact_task_check_deadline":               "deadline",
	"ui.compact_task_sleep":                        "sleep",
	"ui.wait_label_mon":                            "MON",
	"ui.wait_label_s1":                             "S1",
	"ui.wait_label_s2":                             "S2",
	"ui.wait_label_s3":                             "S3",
	"ui.wait_label_wait":                           "WAIT",
}

var koreanStrings = map[string]string{
	"app.locale.name":                              "한국어",
	"app.default_continue_message":                 "응 계속 이어서 진행해서 포팅 완료까지 진행해보자",
	"cmd.default_continue_message":                 "응 계속 이어서 진행해서 포팅 완료까지 진행해보자",
	"cmd.watch_start":                              "감시 시작: session=%s interval=%ds duration=%ds",
	"cmd.watch_llm_status":                         "llm=%s model=%s fallback_llm=%s fallback_model=%s policy=%s capture=%d suspect_waits=%ds/%ds/%ds",
	"cmd.check_llm_status":                         "llm=%s model=%s fallback_llm=%s fallback_model=%s",
	"cmd.check_provider_selected":                  "선택된 LLM provider: %s",
	"cmd.watch_dryrun":                             "dry-run=%t format=%s",
	"cmd.watch_auto_update_flag":                   "auto-update=%t",
	"cmd.watch_auto_update_error":                  "자동 업데이트 처리 중 오류: %v",
	"cmd.watch_tmux_target":                        "tmux 타겟: %s",
	"cmd.watch_current_sessions":                   "현재 tmux 세션:",
	"cmd.watch_session_item":                       "  - %s",
	"cmd.watch_end":                                "감시 종료",
	"cmd.watch_auto_update_applied":                "자동 업데이트 적용됨: %s -> %s",
	"cmd.watch_auto_update_restart":                "자기 갱신으로 새 프로세스를 실행합니다.",
	"cmd.check_start":                              "1회 검사 시작: session=%s capture=%d",
	"cmd.check_offline_mode":                       "오프라인 판정 모드: capture file=%s",
	"cmd.check_offline_empty":                      "오프라인 체크 완료: 빈 캡처",
	"cmd.check_result":                             "체크 결과: %s / %s",
	"cmd.check_session_not_found":                  "결과: 세션이 존재하지 않습니다",
	"cmd.watchstate_default":                       "watch state",
	"cmd.error_interval":                           "interval-seconds는 0보다 커야 합니다",
	"cmd.error_suspect_wait_1":                     "suspect-wait-seconds-1은 0보다 커야 합니다",
	"cmd.error_suspect_wait_2":                     "suspect-wait-seconds-2은 0보다 커야 합니다",
	"cmd.error_suspect_wait_3":                     "suspect-wait-seconds-3은 0보다 커야 합니다",
	"cmd.error_duration":                           "duration-seconds는 0보다 커야 합니다",
	"cmd.error_capture_lines":                      "capture-lines는 0보다 커야 합니다",
	"cmd.error_watch_state_dir":                    "watch state 디렉터리 생성 실패: %v",
	"cmd.error_tmux_session_list":                  "tmux 세션 목록 조회 실패: %v",
	"cmd.error_capture_file_read":                  "capture 파일 읽기 실패 (%s): %v",
	"cmd.error_tmux_capture_failed":                "tmux 패널 캡처 실패 (session=%s): %v",
	"cmd.warn_tmux_session_list_failed":            "경고: tmux 세션 목록 조회 실패: %v",
	"cmd.warn_tmux_session_fallback":               "경고: tmux 세션 확인 폴백 실패: %v",
	"cmd.warn_session_state_prompt_save":           "경고: 세션 상태 프롬프트 저장 실패 (%s): %s",
	"cmd.watch_session_state_request":              "LLM 요청 시작: session-state 판정 (binary=%s, prompt_len=%d)",
	"cmd.watch_session_state_response_fail":        "LLM 응답 완료: session-state 판정 실패 (%v) elapsed=%s",
	"cmd.watch_session_state_response_ok":          "LLM 응답 완료: session-state 판정 성공 (elapsed=%s)",
	"cmd.warn_session_state_result_save":           "경고: 세션 상태 결과 저장 실패 (%s): %s",
	"cmd.warn_session_state_parse_failed":          "경고: 세션 판정 파싱 실패: %s",
	"cmd.warn_classify_prompt_save":                "경고: 판정 프롬프트 저장 실패 (%s): %s",
	"cmd.warn_classify_result_save":                "경고: 판정 결과 저장 실패 (%s): %s",
	"cmd.reason_capture_read_failed":               "캡처 읽기 실패",
	"cmd.reason_empty_capture":                     "캡처 결과가 비어 있어 판정 보류",
	"cmd.reason_completed_fixture":                 "capture 파일명이 completed fixture를 가리킴: %s",
	"cmd.reason_progress_default_evidence":         "프롬프트 위치를 기반으로 추정",
	"cmd.reason_working":                           "작업 중으로 판단: %s",
	"cmd.reason_llm_exec_failed":                   "llm 실행 실패: %s",
	"cmd.reason_no_reason":                         "사유 없음",
	"cmd.classify_request":                         "LLM 요청 시작: NEED_CONTINUE 판정 (%s, binary=%s, prompt_len=%d)",
	"cmd.classify_response_fail":                   "LLM 응답 완료: NEED_CONTINUE 판정 실패 (%v) elapsed=%s",
	"cmd.classify_response_ok":                     "LLM 응답 완료: NEED_CONTINUE 판정 성공 (elapsed=%s)",
	"watch.reason_completion_ready_transition":     "판정 후처리: completion-ready signal 감지로 WORKING->COMPLETED 강제 전환",
	"cmd.log_llm_usage":                            "LLM 사용량(%s): source=%s, remaining=%d",
	"cmd.log_llm_usage_unknown":                    "LLM 사용량(%s): 공급자가 제공하지 않음 (source=%s)",
	"cmd.log_llm_selected":                         "LLM(%s): %s (%s)",
	"cmd.error_no_llm_provider":                    "사용 가능한 llm provider가 없습니다",
	"cmd.error_llm_provider_init_failed":           "llm provider 초기화 실패: %s",
	"cmd.warn_capture_artifacts_dir":               "경고: captured 디렉터리 생성 실패 (%s): %s",
	"cmd.warn_capture_raw_write":                   "경고: captured ANSI 저장 실패 (%s): %s",
	"cmd.warn_capture_plain_write":                 "경고: captured plain 저장 실패 (%s): %s",
	"cmd.warn_capture_decision_marshal":            "경고: 판정 JSON marshal 실패: %s",
	"cmd.warn_capture_decision_json_write":         "경고: 판정 JSON 저장 실패 (%s): %s",
	"cmd.warn_state_file_dir_mkdir":                "경고: 상태 파일 디렉터리 생성 실패 (%s): %s",
	"cmd.warn_capture_text_write":                  "경고: 캡처 결과 파일 저장 실패 (%s): %s",
	"watch.task_check_deadline_description":        "watch deadline exceeded 여부를 확인한다",
	"watch.task_base_capture_description":          "기준 주기 dual capture를 수행한다",
	"watch.task_watch_capture_description":         "watch 주기 dual capture 수행",
	"watch.task_ansi_recheck_description":          "%d차 입력 대기 의심 상태에서 ANSI 재비교를 수행한다",
	"watch.task_interpret_force_input_description": "장기 정지 ANSI 화면을 강제 입력 대상으로 해석해 다음 동작을 결정한다",
	"watch.task_interpret_interactive_description": "interactive prompt를 우선시해 변동 ANSI를 허용하고 다음 동작을 결정한다",
	"watch.task_interpret_default_description":     "prompt 위치와 마지막 출력 블록을 해석해 다음 동작을 결정한다",
	"watch.state_once_interpret":                   "once mode: 현재 화면을 즉시 해석",
	"watch.state_once_no_assistant":                "once mode: assistant UI 시그니처가 없어 추가 입력 없이 종료",
	"watch.state_once_copilot_clear":               "once mode: Copilot slash command 상태를 정리하고 종료",
	"watch.state_once_processing":                  "once mode: 진행중 신호가 감지되어 추가 입력 없이 종료",
	"watch.state_once_replace_copilot":             "once mode: Copilot 입력창의 slash-start 미실행 텍스트를 비우고 새 continue 입력으로 교체",
	"watch.state_once_submit_pending":              "once mode: 입력창에 미실행 텍스트가 남아 있어 submit-only 실행",
	"watch.state_once_plan":                        "once mode: 결정적 입력 계획을 적용",
	"watch.state_once_llm_fail":                    "once mode: LLM 해석 실패로 기본 continue 입력",
	"watch.state_once_unknown":                     "once mode: 미분류 대기 상태로 판단되어 기본 continue 입력",
	"watch.state_watch_monitoring":                 "모니터링",
	"watch.state_monitoring":                       "모니터링",
	"watch.reason_bypass_prompt_wait":              "하단 interactive prompt가 감지되어 ANSI 안정성 대기를 우회하고 해석으로 진입",
	"watch.reason_capture_capture_init":            "기준 ANSI 캡처를 저장하고 다음 %s 주기로 모니터링 대기",
	"watch.reason_watch_deadline_exceeded":         "watch deadline이 초과됨",
	"watch.reason_capture_changed":                 "ANSI 화면이 이전 기준 캡처와 달라짐, 다시 %s 모니터링",
	"watch.reason_capture_changed_wait":            "ANSI 변경 감지, 다시 %s 기준 캡처 대기",
	"watch.reason_stable_promote":                  "ANSI 화면이 최근 %s 이상 동일하게 유지되어 즉시 입력 대기 상태로 승격",
	"watch.reason_suspect_initial":                 "ANSI 화면이 %s 기준 캡처와 동일하여 1차 입력 대기 의심",
	"watch.reason_suspect_wait":                    "%d차 입력 대기 의심, %s 후 ANSI 재확인",
	"watch.reason_min_stable_time":                 "ANSI 화면은 동일하며 최소 안정 시간 %s를 채우기 위해 %d차 입력 대기 의심으로 진행",
	"watch.reason_confident_by_repetition":         "16초 동안 5회 ANSI 화면이 동일하여 입력 대기 상태를 확신",
	"watch.interpret_force_input_description":      "장기 정지 ANSI 화면을 강제 입력 대상으로 해석해 다음 동작을 결정한다",
	"watch.interpret_interactive_description":      "interactive prompt를 우선시해 변동 ANSI를 허용하고 다음 동작을 결정한다",
	"watch.interpret_default_description":          "prompt 위치와 마지막 출력 블록을 해석해 다음 동작을 결정한다",
	"watch.interpret_start":                        "waiting 화면 해석 시작",
	"watch.interpret_prompt_changed":               "해석 직전 화면이 바뀌어 모니터링으로 복귀",
	"watch.interpret_prompt_changed_wait":          "해석 직전 ANSI 변경 감지, 다시 %s 모니터링",
	"watch.interpret_assistant_signature_weak":     "장시간 동일 ANSI 화면에서 assistant UI 시그니처는 약하지만 강제 continue 입력",
	"watch.interpret_no_assistant_monitor":         "assistant UI 시그니처가 없어 입력하지 않고 %s 모니터링으로 복귀",
	"watch.interpret_copilot_clear":                "Copilot slash command 상태를 정리하고 모니터링으로 복귀",
	"watch.interpret_processing_force_continue":    "장시간 동일 ANSI 화면이 processing으로 보이지만 정체 상태로 판단되어 강제 continue 입력",
	"watch.interpret_processing_wait":              "진행중 신호가 남아 있어 입력하지 않고 %s 모니터링으로 복귀",
	"watch.interpret_replace_pending":              "Copilot 입력창의 slash-start 미실행 텍스트를 비우고 새 continue 입력으로 교체",
	"watch.interpret_submit_pending":               "입력창에 미실행 텍스트가 남아 있어 submit-only 실행",
	"watch.interpret_plan":                         "결정적 입력 계획을 적용",
	"watch.interpret_llm_fail":                     "LLM 해석 실패로 기본 continue 입력",
	"watch.interpret_unknown":                      "미분류 대기 상태로 판단되어 기본 continue 입력",
	"watch.interpret_force_input_override":         "장시간 동일 ANSI 화면에서 강제 continue 입력",
	"watch.sleep_capture_init":                     "기준 ANSI 캡처 저장 완료; 다음 %s 주기 대기",
	"watch.sleep_capture_changed":                  "캡처 변경 감지; 다음 %s 주기로 모니터링 계속",
	"watch.sleep_interpret_prompt_changed":         "해석 직전 ANSI 변경 감지; %s 대기 후 재확인",
	"watch.sleep_no_assistant_monitor":             "assistant UI 미확인; %s 대기 후 모니터링 계속",
	"watch.sleep_processing_wait":                  "진행중 신호 감지; %s 대기 후 모니터링 계속",
	"watch.sleep_after_send":                       "입력 전송 후 다음 %s 모니터링 대기",
	"watch.sleep_after_choice":                     "선택지 입력 후 다음 %s 모니터링 대기",
	"watch.sleep_after_cursor":                     "커서형 선택 확정 후 다음 %s 모니터링 대기",
	"watch.sleep_after_submit":                     "submit-only 실행 후 다음 %s 모니터링 대기",
	"watch.sleep_after_clear":                      "입력창 정리 후 다음 %s 모니터링 대기",
	"watch.sleep_after_input_value":                "입력값 전송 후 다음 %s 모니터링 대기",
	"watch.once_after_input":                       "once mode: continue 입력 후 종료",
	"watch.once_after_choice":                      "once mode: 선택 입력 후 종료",
	"watch.once_after_cursor":                      "once mode: 커서형 선택 확정 후 종료",
	"watch.once_after_input_submit":                "once mode: 입력값 전송 후 종료",
	"watch.once_after_submit":                      "once mode: submit-only 실행 후 종료",
	"watch.once_after_clear":                       "once mode: 입력창 정리 후 종료",
	"watch.fallback_empty_input":                   "빈 free-text 입력 추천이 반환되어 기본 continue 입력으로 폴백",
	"watch.fallback_skip":                          "%s SKIP 대신 continue 입력으로 폴백",
	"watch.fallback_skip_watch":                    "LLM이 SKIP 결정을 반환해도 continue 입력으로 폴백",
	"watch.fallback_cursor":                        "%s 방향키 선택이 확인되지 않아 continue 입력으로 폴백",
	"watch.plan_remaining_items":                   "남은 항목 %s번(%s)부터 진행하고, 검증까지 마친 뒤 다음 항목으로 이어서 진행해보자.",
	"watch.waiting_reason_force_input":             "동일 ANSI 안정 상태에서 강제 continue를 수행합니다",
	"executor.intent_continue":                     "continue 메세지로 free text 입력 전송",
	"executor.intent_choice":                       "숫자 선택지를 선택 후 제출",
	"executor.intent_cursor":                       "커서 이동으로 항목을 선택해 제출",
	"executor.intent_input":                        "free text 입력",
	"executor.intent_submit_pending":               "미입력 텍스트 submit-only 처리",
	"executor.intent_clear_prompt":                 "프롬프트 정리",
	"executor.cursor_note_probe":                   "cursor probe: down then up to verify first row",
	"executor.cursor_move":                         "cursor move: down %d",
	"executor.plan_output_action":                  "action=%s",
	"executor.plan_output_intent":                  "intent=%s",
	"ui.value_none":                                "-",
	"ui.card_title_session":                        "세션",
	"ui.card_title_flow":                           "흐름",
	"ui.card_title_control":                        "제어",
	"ui.card_title_timing":                         "타이밍",
	"ui.card_title_ai":                             "AI",
	"ui.card_title_detail":                         "상세",
	"ui.label_target":                              "target",
	"ui.label_mode":                                "mode",
	"ui.label_capture":                             "capture",
	"ui.label_state":                               "state",
	"ui.label_now":                                 "현재",
	"ui.label_queued":                              "queued",
	"ui.label_next":                                "다음",
	"ui.label_wait":                                "대기",
	"ui.label_sleep":                               "대기",
	"ui.label_expire":                              "만료",
	"ui.label_policy":                              "정책",
	"ui.label_llm":                                 "llm",
	"ui.label_continue":                            "cont",
	"ui.label_doing":                               "doing",
	"ui.label_event":                               "event",
	"ui.label_next_action":                         "next",
	"ui.log_panel_title":                           "활동 로그",
	"ui.sleep_status":                              "%s / %s (%s 남음)",
	"ui.updated_label":                             "updated",
	"ui.just_now":                                  "방금 전",
	"ui.ago":                                       "%s (%s 전)",
	"ui.compact_state_monitoring":                  "모니터",
	"ui.compact_state_suspect_waiting_stage_1":     "대기1",
	"ui.compact_state_suspect_waiting_stage_2":     "대기2",
	"ui.compact_state_suspect_waiting_stage_3":     "대기3",
	"ui.compact_state_confident_waiting":           "대기",
	"ui.compact_state_interpreting":                "해석",
	"ui.compact_state_acting":                      "입력",
	"ui.compact_state_completed":                   "완료",
	"ui.compact_state_stopped":                     "중지",
	"ui.compact_task_capture":                      "캡처",
	"ui.compact_task_compare_capture":              "비교",
	"ui.compact_task_interpret":                    "해석",
	"ui.compact_task_check_deadline":               "마감",
	"ui.compact_task_sleep":                        "sleep",
	"ui.wait_label_mon":                            "MON",
	"ui.wait_label_s1":                             "S1",
	"ui.wait_label_s2":                             "S2",
	"ui.wait_label_s3":                             "S3",
	"ui.wait_label_wait":                           "WAIT",
}

func SupportedLocales() []string {
	return []string{"en", "ko", "ja", "zh", "vi", "hi", "ru", "es", "fr"}
}

func NormalizeLocale(raw string) string {
	locale := strings.ToLower(strings.TrimSpace(raw))
	locale = strings.ReplaceAll(locale, "_", "-")
	if locale == "" {
		return DefaultAppLocale
	}
	if resolved, ok := localeAliases[locale]; ok {
		return resolved
	}
	if strings.HasPrefix(locale, "zh-") {
		return "zh"
	}
	if strings.HasPrefix(locale, "en-") {
		return "en"
	}
	if strings.HasPrefix(locale, "pt-") {
		return "en"
	}
	return locale
}

func IsSupportedLocale(raw string) bool {
	locale := NormalizeLocale(raw)
	if _, ok := translations[locale]; ok {
		return true
	}
	if overrides, ok := localeMessages[locale]; ok && len(overrides) > 0 {
		return true
	}
	return false
}

func T(locale string, key string, args ...interface{}) string {
	loc := NormalizeLocale(locale)
	if overrides := localeMessages[loc]; len(overrides) > 0 {
		if value, ok := overrides[key]; ok && strings.TrimSpace(value) != "" {
			if len(args) == 0 {
				return value
			}
			return fmt.Sprintf(value, args...)
		}
	}
	if loc != DefaultLocale {
		if overrides := localeMessages[DefaultLocale]; len(overrides) > 0 {
			if value, ok := overrides[key]; ok && strings.TrimSpace(value) != "" {
				if len(args) == 0 {
					return value
				}
				return fmt.Sprintf(value, args...)
			}
		}
	}
	msgs, ok := translations[loc]
	if !ok || msgs == nil {
		msgs = translations[DefaultLocale]
	}
	value := msgs[key]
	if value == "" {
		value = translations[DefaultLocale][key]
	}
	if value == "" {
		return key
	}
	if len(args) == 0 {
		return value
	}
	return fmt.Sprintf(value, args...)
}

func init() {
	if len(embeddedMessages) == 0 {
		return
	}
	raw := make(map[string]map[string]string)
	if err := json.Unmarshal(embeddedMessages, &raw); err != nil {
		return
	}
	for locale, entries := range raw {
		locale = NormalizeLocale(locale)
		if locale == "" {
			continue
		}
		cleaned := map[string]string{}
		for k, v := range entries {
			key := strings.TrimSpace(k)
			value := strings.TrimSpace(v)
			if key == "" || value == "" {
				continue
			}
			cleaned[key] = value
		}
		if len(cleaned) > 0 {
			localeMessages[locale] = cleaned
		}
	}
}
