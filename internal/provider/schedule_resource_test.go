package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
)

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "serviceerror.NotFound",
			err:  serviceerror.NewNotFound("schedule not found"),
			want: true,
		},
		{
			name: "wrapped serviceerror.NotFound",
			err:  fmt.Errorf("outer: %w", serviceerror.NewNotFound("schedule not found")),
			want: true,
		},
		{
			name: "other error",
			err:  fmt.Errorf("some random error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotFoundError(tt.err)
			if got != tt.want {
				t.Errorf("isNotFoundError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func startDevServer(t *testing.T) *testsuite.DevServer {
	t.Helper()
	server, err := testsuite.StartDevServer(context.Background(), testsuite.DevServerOptions{
		LogLevel:     "error",
		ExistingPath: os.Getenv("TEMPORAL_CLI_PATH"),
	})
	if err != nil {
		t.Fatalf("starting Temporal dev server: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Logf("stopping Temporal dev server: %v", err)
		}
	})
	return server
}

func TestAccSchedule_BasicCron(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-cron-schedule"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "action.workflow_type", "TestWorkflow"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "action.task_queue", "test-queue"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "overlap_policy", "skip"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "is_paused", "false"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "drift_detected", "false"),
				),
			},
		},
	})
}

func TestAccSchedule_Calendar(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_calendar(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-calendar-schedule"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "action.workflow_type", "TestWorkflow"),
				),
			},
		},
	})
}

func TestAccSchedule_Interval(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_interval(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-interval-schedule"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "action.workflow_type", "TestWorkflow"),
				),
			},
		},
	})
}

func TestAccSchedule_Update(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-cron-schedule"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "spec.cron_expressions.0", "0 2 * * *"),
				),
			},
			{
				Config: testAccScheduleConfig_updatedCron(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-cron-schedule"),
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "spec.cron_expressions.0", "0 3 * * *"),
				),
			},
		},
	})
}

func TestAccSchedule_PauseUnpause(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_paused(server.FrontendHostPort(), true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "is_paused", "true"),
				),
			},
			{
				Config: testAccScheduleConfig_paused(server.FrontendHostPort(), false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "is_paused", "false"),
				),
			},
		},
	})
}

func TestAccSchedule_Destroy(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-cron-schedule"),
				),
			},
		},
	})
}

func TestAccSchedule_Import(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_basicCron(server.FrontendHostPort()),
			},
			{
				Config:        testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				ResourceName:  "temporalschedules_schedule.test",
				ImportState:   true,
				ImportStateId: "default/test-cron-schedule",
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}
					s := states[0].Attributes
					if s["namespace"] != "default" {
						return fmt.Errorf("expected namespace 'default', got %q", s["namespace"])
					}
					if s["name"] != "test-cron-schedule" {
						return fmt.Errorf("expected name 'test-cron-schedule', got %q", s["name"])
					}
					return nil
				},
			},
		},
	})
}

func TestAccSchedule_RecreateAfterExternalDeletion(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-cron-schedule"),
				),
			},
			{
				// Delete the schedule externally via the Temporal SDK
				PreConfig: func() {
					tc, err := getTemporalClient(context.Background(), server.FrontendHostPort(), "default", "")
					if err != nil {
						t.Fatalf("creating Temporal client: %v", err)
					}
					handle := tc.ScheduleClient().GetHandle(context.Background(), "test-cron-schedule")
					if err := handle.Delete(context.Background()); err != nil {
						t.Fatalf("deleting schedule externally: %v", err)
					}
				},
				// Same config — Terraform should detect it's gone and recreate it
				Config: testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-cron-schedule"),
				),
			},
		},
	})
}

func TestAccSchedule_DestroyAfterExternalDeletion(t *testing.T) {
	server := startDevServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("temporalschedules_schedule.test", "name", "test-cron-schedule"),
				),
			},
			{
				// Delete the schedule externally, then destroy — should not error
				PreConfig: func() {
					tc, err := getTemporalClient(context.Background(), server.FrontendHostPort(), "default", "")
					if err != nil {
						t.Fatalf("creating Temporal client: %v", err)
					}
					handle := tc.ScheduleClient().GetHandle(context.Background(), "test-cron-schedule")
					if err := handle.Delete(context.Background()); err != nil {
						t.Fatalf("deleting schedule externally: %v", err)
					}
				},
				Config:  testAccScheduleConfig_basicCron(server.FrontendHostPort()),
				Destroy: true,
			},
		},
	})
}

func TestAccSchedule_OverlapPolicy(t *testing.T) {
	server := startDevServer(t)

	for _, policy := range validOverlapPolicies {
		t.Run(policy, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: testAccScheduleConfig_overlapPolicy(server.FrontendHostPort(), policy),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("temporalschedules_schedule.test", "overlap_policy", policy),
						),
					},
				},
			})
		})
	}
}

func testAccScheduleConfig_basicCron(address string) string {
	return fmt.Sprintf(`
provider "temporalschedules" {}

resource "temporalschedules_schedule" "test" {
  namespace = "default"
  address   = %q
  name      = "test-cron-schedule"

  action {
    workflow_type = "TestWorkflow"
    task_queue    = "test-queue"
  }

  spec {
    cron_expressions = ["0 2 * * *"]
  }
}
`, address)
}

func testAccScheduleConfig_updatedCron(address string) string {
	return fmt.Sprintf(`
provider "temporalschedules" {}

resource "temporalschedules_schedule" "test" {
  namespace = "default"
  address   = %q
  name      = "test-cron-schedule"

  action {
    workflow_type = "TestWorkflow"
    task_queue    = "test-queue"
  }

  spec {
    cron_expressions = ["0 3 * * *"]
  }
}
`, address)
}

func testAccScheduleConfig_calendar(address string) string {
	return fmt.Sprintf(`
provider "temporalschedules" {}

resource "temporalschedules_schedule" "test" {
  namespace = "default"
  address   = %q
  name      = "test-calendar-schedule"

  action {
    workflow_type = "TestWorkflow"
    task_queue    = "test-queue"
  }

  spec {
    calendar {
      hour         = "2"
      minute       = "0"
      day_of_month = "1"
      month        = "1,6"
    }
  }
}
`, address)
}

func testAccScheduleConfig_interval(address string) string {
	return fmt.Sprintf(`
provider "temporalschedules" {}

resource "temporalschedules_schedule" "test" {
  namespace = "default"
  address   = %q
  name      = "test-interval-schedule"

  action {
    workflow_type = "TestWorkflow"
    task_queue    = "test-queue"
  }

  spec {
    interval {
      every  = "4h"
      offset = "30m"
    }
  }
}
`, address)
}

func testAccScheduleConfig_paused(address string, paused bool) string {
	return fmt.Sprintf(`
provider "temporalschedules" {}

resource "temporalschedules_schedule" "test" {
  namespace = "default"
  address   = %q
  name      = "test-pause-schedule"

  action {
    workflow_type = "TestWorkflow"
    task_queue    = "test-queue"
  }

  spec {
    cron_expressions = ["0 2 * * *"]
  }

  is_paused = %t
}
`, address, paused)
}

func testAccScheduleConfig_overlapPolicy(address, policy string) string {
	return fmt.Sprintf(`
provider "temporalschedules" {}

resource "temporalschedules_schedule" "test" {
  namespace = "default"
  address   = %q
  name      = "test-overlap-%s"

  action {
    workflow_type = "TestWorkflow"
    task_queue    = "test-queue"
  }

  spec {
    cron_expressions = ["0 2 * * *"]
  }

  overlap_policy = %q
}
`, address, policy, policy)
}

func TestScheduleFingerprint(t *testing.T) {
	newSched := func() *client.ScheduleDescription {
		return &client.ScheduleDescription{Schedule: client.Schedule{
			Spec:  &client.ScheduleSpec{Calendars: []client.ScheduleCalendarSpec{{Hour: []client.ScheduleRange{{Start: 2, End: 2}}}}},
			State: &client.ScheduleState{},
		}}
	}
	base, err := scheduleFingerprint(newSched())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := scheduleFingerprint(newSched()); !bytes.Equal(got, base) {
		t.Errorf("fingerprint not stable: %s != %s", got, base)
	}
	paused := newSched()
	paused.Schedule.State = &client.ScheduleState{Paused: true, Note: "maintenance"}
	if got, _ := scheduleFingerprint(paused); !bytes.Equal(got, base) {
		t.Errorf("State must not change the fingerprint")
	}
	changed := newSched()
	changed.Schedule.Spec.Calendars[0].Hour[0].Start = 3
	if got, _ := scheduleFingerprint(changed); bytes.Equal(got, base) {
		t.Errorf("changing spec must change the fingerprint")
	}
	for stored, want := range map[string]bool{string(base): true, "": false, `"abc"`: false, `{"deps":"go.temporal.io/sdk@v0.0.0","hash":"x"}`: false} {
		if got := sameDeps([]byte(stored)); got != want {
			t.Errorf("sameDeps(%s) = %t, want %t", stored, got, want)
		}
	}
}

func driftScheduleHandle(t *testing.T, address string) client.ScheduleHandle {
	tc, err := getTemporalClient(context.Background(), address, "default", "")
	if err != nil {
		t.Fatalf("creating Temporal client: %v", err)
	}
	return tc.ScheduleClient().GetHandle(context.Background(), "test-drift-schedule")
}

func updateScheduleOutOfBand(t *testing.T, address string, edit func(*client.Schedule)) {
	err := driftScheduleHandle(t, address).Update(context.Background(), client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			s := input.Description.Schedule
			edit(&s)
			return &client.ScheduleUpdate{Schedule: &s}, nil
		},
	})
	if err != nil {
		t.Fatalf("updating schedule out of band: %v", err)
	}
}

func checkSchedule(t *testing.T, address string, ok func(*client.Schedule) bool) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		desc, err := driftScheduleHandle(t, address).Describe(context.Background())
		if err != nil {
			return err
		}
		if !ok(&desc.Schedule) {
			b, _ := json.Marshal(desc.Schedule)
			return fmt.Errorf("unexpected schedule: %s", b)
		}
		return nil
	}
}

func TestAccSchedule_Drift(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	config := testAccScheduleConfig_drift(address, "", 2)

	for _, tt := range []struct {
		name     string
		edit     func(*client.Schedule)
		restored func(*client.Schedule) bool
	}{
		{"spec", func(s *client.Schedule) { s.Spec = &client.ScheduleSpec{CronExpressions: []string{"0 5 * * *"}} },
			func(s *client.Schedule) bool {
				c := s.Spec.Calendars
				return len(c) == 1 && len(c[0].Hour) == 1 && c[0].Hour[0].Start == 2
			}},
		{"input", func(s *client.Schedule) {
			s.Action.(*client.ScheduleWorkflowAction).Args = []interface{}{map[string]string{"format": "csv"}}
		}, func(s *client.Schedule) bool {
			args := s.Action.(*client.ScheduleWorkflowAction).Args
			return len(args) == 1 && string(args[0].(*commonpb.Payload).GetData()) == `{"format":"pdf"}`
		}},
		{"catchup_window", func(s *client.Schedule) { s.Policy.CatchupWindow = 2 * time.Hour },
			func(s *client.Schedule) bool { return s.Policy.CatchupWindow == time.Hour }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config},
					{
						PreConfig: func() { updateScheduleOutOfBand(t, address, tt.edit) },
						Config:    config,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("temporalschedules_schedule.test", plancheck.ResourceActionUpdate),
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("temporalschedules_schedule.test", "drift_detected", "false"),
							checkSchedule(t, address, tt.restored),
						),
					},
				},
			})
		})
	}
}

func TestAccSchedule_PauseOutsideTerraformIsNotDrift(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	ignorePaused := "lifecycle {\n    ignore_changes = [is_paused]\n  }"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig_drift(address, ignorePaused, 2),
			},
			{
				PreConfig: func() {
					updateScheduleOutOfBand(t, address, func(*client.Schedule) {})
					if err := driftScheduleHandle(t, address).Pause(context.Background(), client.SchedulePauseOptions{Note: "maintenance"}); err != nil {
						t.Fatalf("pausing schedule: %v", err)
					}
				},
				Config:   testAccScheduleConfig_drift(address, ignorePaused, 2),
				PlanOnly: true,
			},
			{
				Config: testAccScheduleConfig_drift(address, ignorePaused, 4),
				Check: checkSchedule(t, address, func(s *client.Schedule) bool {
					c := s.Spec.Calendars
					return s.State.Paused && s.State.Note == "maintenance" && len(c) == 1 && c[0].Hour[0].Start == 4
				}),
			},
		},
	})
}

func TestAccSchedule_UpgradeFromRegistryVersion(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()

	resource.Test(t, resource.TestCase{
		Steps: []resource.TestStep{
			{
				ExternalProviders: map[string]resource.ExternalProvider{
					"temporalschedules": {Source: "rest-capital/temporal-schedules", VersionConstraint: "0.3.9"},
				},
				Config: testAccScheduleConfig_drift(address, "", 2),
			},
			{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   testAccScheduleConfig_drift(address, "", 2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr("temporalschedules_schedule.test", "drift_detected", "false"),
			},
			{
				// Drift is only seen if the baseline from the previous refresh was persisted.
				PreConfig: func() {
					updateScheduleOutOfBand(t, address, func(s *client.Schedule) { s.Spec = &client.ScheduleSpec{CronExpressions: []string{"0 5 * * *"}} })
				},
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   testAccScheduleConfig_drift(address, "", 2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("temporalschedules_schedule.test", plancheck.ResourceActionUpdate)},
				},
				Check: checkSchedule(t, address, func(s *client.Schedule) bool { c := s.Spec.Calendars; return len(c) == 1 && c[0].Hour[0].Start == 2 }),
			},
		},
	})
}

func testAccScheduleConfig_drift(address, extra string, hour int) string {
	return fmt.Sprintf(`
provider "temporalschedules" {}

resource "temporalschedules_schedule" "test" {
  namespace      = "default"
  address        = %q
  name           = "test-drift-schedule"
  catchup_window = "1h"

  action {
    workflow_type = "TestWorkflow"
    task_queue    = "test-queue"
    input_payload = jsonencode({ format = "pdf" })
  }

  spec {
    cron_expressions = ["0 %d * * *"]
  }

  %s
}
`, address, hour, extra)
}
