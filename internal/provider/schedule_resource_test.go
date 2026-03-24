package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"go.temporal.io/api/serviceerror"
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
		LogLevel: "error",
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
