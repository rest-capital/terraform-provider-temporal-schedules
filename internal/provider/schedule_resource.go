package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

var (
	_ resource.Resource                = &scheduleResource{}
	_ resource.ResourceWithImportState = &scheduleResource{}
)

type scheduleResource struct{}

type scheduleResourceModel struct {
	Namespace     types.String            `tfsdk:"namespace"`
	Address       types.String            `tfsdk:"address"`
	APIKey        types.String            `tfsdk:"api_key"`
	Name          types.String            `tfsdk:"name"`
	Action        *actionModel            `tfsdk:"action"`
	Spec          *specModel              `tfsdk:"spec"`
	OverlapPolicy types.String            `tfsdk:"overlap_policy"`
	IsPaused      types.Bool              `tfsdk:"is_paused"`
	CatchupWindow types.String            `tfsdk:"catchup_window"`
	Memo          map[string]types.String `tfsdk:"memo"`
}

type actionModel struct {
	WorkflowType types.String `tfsdk:"workflow_type"`
	TaskQueue    types.String `tfsdk:"task_queue"`
	WorkflowID   types.String `tfsdk:"workflow_id"`
	InputPayload types.String `tfsdk:"input_payload"`
}

type specModel struct {
	CronExpressions []types.String  `tfsdk:"cron_expressions"`
	Calendars       []calendarModel `tfsdk:"calendar"`
	Intervals       []intervalModel `tfsdk:"interval"`
	Timezone        types.String    `tfsdk:"timezone"`
	Jitter          types.String    `tfsdk:"jitter"`
	StartAt         types.String    `tfsdk:"start_at"`
	EndAt           types.String    `tfsdk:"end_at"`
}

type calendarModel struct {
	Year       types.String `tfsdk:"year"`
	Month      types.String `tfsdk:"month"`
	DayOfMonth types.String `tfsdk:"day_of_month"`
	DayOfWeek  types.String `tfsdk:"day_of_week"`
	Hour       types.String `tfsdk:"hour"`
	Minute     types.String `tfsdk:"minute"`
	Second     types.String `tfsdk:"second"`
	Comment    types.String `tfsdk:"comment"`
}

type intervalModel struct {
	Every  types.String `tfsdk:"every"`
	Offset types.String `tfsdk:"offset"`
}

func NewScheduleResource() resource.Resource {
	return &scheduleResource{}
}

func (r *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func (r *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Temporal Schedule.",
		Attributes: map[string]schema.Attribute{
			"namespace": schema.StringAttribute{
				Required:    true,
				Description: "Temporal namespace.",
			},
			"address": schema.StringAttribute{
				Required:    true,
				Description: "Temporal gRPC endpoint address.",
			},
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Temporal API key. Required for Temporal Cloud.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Schedule identifier.",
			},
			"overlap_policy": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("skip"),
				Description: "Overlap policy: skip, buffer_one, buffer_all, cancel_other, terminate_other, allow_all.",
				Validators: []validator.String{
					stringvalidator.OneOf(validOverlapPolicies...),
				},
			},
			"is_paused": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the schedule is paused.",
			},
			"catchup_window": schema.StringAttribute{
				Optional:    true,
				Description: "Catchup window duration (e.g. '365d', '1h').",
			},
			"memo": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Memo key-value pairs.",
			},
		},
		Blocks: map[string]schema.Block{
			"action": schema.SingleNestedBlock{
				Description: "Workflow action to execute.",
				Attributes: map[string]schema.Attribute{
					"workflow_type": schema.StringAttribute{
						Required:    true,
						Description: "Workflow type name.",
					},
					"task_queue": schema.StringAttribute{
						Required:    true,
						Description: "Task queue name.",
					},
					"workflow_id": schema.StringAttribute{
						Optional:    true,
						Description: "Workflow execution ID. Defaults to a generated ID.",
					},
					"input_payload": schema.StringAttribute{
						Optional:    true,
						Description: "JSON-encoded workflow input.",
					},
				},
			},
			"spec": schema.SingleNestedBlock{
				Description: "Schedule specification.",
				Attributes: map[string]schema.Attribute{
					"cron_expressions": schema.ListAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Cron expressions.",
					},
					"timezone": schema.StringAttribute{
						Optional:    true,
						Description: "IANA timezone name.",
					},
					"jitter": schema.StringAttribute{
						Optional:    true,
						Description: "Random jitter duration.",
					},
					"start_at": schema.StringAttribute{
						Optional:    true,
						Description: "Start time (RFC3339).",
					},
					"end_at": schema.StringAttribute{
						Optional:    true,
						Description: "End time (RFC3339).",
					},
				},
				Blocks: map[string]schema.Block{
					"calendar": schema.ListNestedBlock{
						Description: "Calendar-based schedule spec.",
						NestedObject: schema.NestedBlockObject{
							Attributes: map[string]schema.Attribute{
								"year":         schema.StringAttribute{Optional: true, Description: "Year range."},
								"month":        schema.StringAttribute{Optional: true, Description: "Month range (1-12)."},
								"day_of_month": schema.StringAttribute{Optional: true, Description: "Day of month range (1-31)."},
								"day_of_week":  schema.StringAttribute{Optional: true, Description: "Day of week range (0-6, 0=Sunday)."},
								"hour":         schema.StringAttribute{Optional: true, Description: "Hour range (0-23)."},
								"minute":       schema.StringAttribute{Optional: true, Description: "Minute range (0-59)."},
								"second":       schema.StringAttribute{Optional: true, Description: "Second range (0-59)."},
								"comment":      schema.StringAttribute{Optional: true, Description: "Description of this calendar spec."},
							},
						},
					},
					"interval": schema.ListNestedBlock{
						Description: "Interval-based schedule spec.",
						NestedObject: schema.NestedBlockObject{
							Attributes: map[string]schema.Attribute{
								"every":  schema.StringAttribute{Required: true, Description: "Repeat interval (e.g. '4h', '30m')."},
								"offset": schema.StringAttribute{Optional: true, Description: "Offset from the interval (e.g. '5m')."},
							},
						},
					},
				},
			},
		},
	}
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tc, err := getTemporalClient(ctx, plan.Address.ValueString(), plan.Namespace.ValueString(), plan.APIKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Temporal client", err.Error())
		return
	}

	scheduleSpec, diags := buildScheduleSpec(plan.Spec)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	action, diags := buildWorkflowAction(plan.Action)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := client.ScheduleOptions{
		ID:      plan.Name.ValueString(),
		Spec:    scheduleSpec,
		Action:  action,
		Overlap: toOverlapPolicy(plan.OverlapPolicy.ValueString()),
		Paused:  plan.IsPaused.ValueBool(),
	}

	if !plan.CatchupWindow.IsNull() && !plan.CatchupWindow.IsUnknown() {
		d, err := parseDuration(plan.CatchupWindow.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid catchup_window", err.Error())
			return
		}
		opts.CatchupWindow = d
	}

	if plan.Memo != nil {
		opts.Memo = make(map[string]interface{}, len(plan.Memo))
		for k, v := range plan.Memo {
			opts.Memo[k] = v.ValueString()
		}
	}

	_, err = tc.ScheduleClient().Create(ctx, opts)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.Address.IsNull() || state.Address.ValueString() == "" {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	tc, err := getTemporalClient(ctx, state.Address.ValueString(), state.Namespace.ValueString(), state.APIKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Temporal client", err.Error())
		return
	}

	handle := tc.ScheduleClient().GetHandle(ctx, state.Name.ValueString())
	desc, err := handle.Describe(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to describe schedule", err.Error())
		return
	}

	readScheduleIntoState(&state, desc)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan scheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tc, err := getTemporalClient(ctx, plan.Address.ValueString(), plan.Namespace.ValueString(), plan.APIKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Temporal client", err.Error())
		return
	}

	scheduleSpec, diags := buildScheduleSpec(plan.Spec)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	action, diags := buildWorkflowAction(plan.Action)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	overlapPolicy := toOverlapPolicy(plan.OverlapPolicy.ValueString())

	var catchupWindow time.Duration
	if !plan.CatchupWindow.IsNull() && !plan.CatchupWindow.IsUnknown() {
		catchupWindow, err = parseDuration(plan.CatchupWindow.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid catchup_window", err.Error())
			return
		}
	}

	handle := tc.ScheduleClient().GetHandle(ctx, plan.Name.ValueString())
	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(_ client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			return &client.ScheduleUpdate{
				Schedule: &client.Schedule{
					Action: action,
					Spec:   &scheduleSpec,
					Policy: &client.SchedulePolicies{
						Overlap:       overlapPolicy,
						CatchupWindow: catchupWindow,
					},
					State: &client.ScheduleState{
						Paused: plan.IsPaused.ValueBool(),
					},
				},
			}, nil
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tc, err := getTemporalClient(ctx, state.Address.ValueString(), state.Namespace.ValueString(), state.APIKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Temporal client", err.Error())
		return
	}

	handle := tc.ScheduleClient().GetHandle(ctx, state.Name.ValueString())
	err = handle.Delete(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete schedule", err.Error())
		return
	}
}

func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "Expected format: namespace/schedule-name")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
}

func buildScheduleSpec(spec *specModel) (client.ScheduleSpec, diag.Diagnostics) {
	var result client.ScheduleSpec
	var diags diag.Diagnostics

	if spec == nil {
		return result, diags
	}

	for _, expr := range spec.CronExpressions {
		result.CronExpressions = append(result.CronExpressions, expr.ValueString())
	}

	for _, cal := range spec.Calendars {
		calSpec := client.ScheduleCalendarSpec{
			Comment: cal.Comment.ValueString(),
		}
		if !cal.Second.IsNull() && !cal.Second.IsUnknown() {
			calSpec.Second = parseRangeString(cal.Second.ValueString())
		}
		if !cal.Minute.IsNull() && !cal.Minute.IsUnknown() {
			calSpec.Minute = parseRangeString(cal.Minute.ValueString())
		}
		if !cal.Hour.IsNull() && !cal.Hour.IsUnknown() {
			calSpec.Hour = parseRangeString(cal.Hour.ValueString())
		}
		if !cal.DayOfMonth.IsNull() && !cal.DayOfMonth.IsUnknown() {
			calSpec.DayOfMonth = parseRangeString(cal.DayOfMonth.ValueString())
		}
		if !cal.Month.IsNull() && !cal.Month.IsUnknown() {
			calSpec.Month = parseRangeString(cal.Month.ValueString())
		}
		if !cal.Year.IsNull() && !cal.Year.IsUnknown() {
			calSpec.Year = parseRangeString(cal.Year.ValueString())
		}
		if !cal.DayOfWeek.IsNull() && !cal.DayOfWeek.IsUnknown() {
			calSpec.DayOfWeek = parseRangeString(cal.DayOfWeek.ValueString())
		}
		result.Calendars = append(result.Calendars, calSpec)
	}

	for _, intv := range spec.Intervals {
		every, err := parseDuration(intv.Every.ValueString())
		if err != nil {
			diags.AddError("Invalid interval.every", err.Error())
			return result, diags
		}
		interval := client.ScheduleIntervalSpec{Every: every}
		if !intv.Offset.IsNull() && !intv.Offset.IsUnknown() {
			offset, err := parseDuration(intv.Offset.ValueString())
			if err != nil {
				diags.AddError("Invalid interval.offset", err.Error())
				return result, diags
			}
			interval.Offset = offset
		}
		result.Intervals = append(result.Intervals, interval)
	}

	if !spec.Timezone.IsNull() && !spec.Timezone.IsUnknown() {
		result.TimeZoneName = spec.Timezone.ValueString()
	}

	if !spec.Jitter.IsNull() && !spec.Jitter.IsUnknown() {
		jitter, err := parseDuration(spec.Jitter.ValueString())
		if err != nil {
			diags.AddError("Invalid jitter", err.Error())
			return result, diags
		}
		result.Jitter = jitter
	}

	if !spec.StartAt.IsNull() && !spec.StartAt.IsUnknown() {
		t, err := time.Parse(time.RFC3339, spec.StartAt.ValueString())
		if err != nil {
			diags.AddError("Invalid start_at", err.Error())
			return result, diags
		}
		result.StartAt = t
	}

	if !spec.EndAt.IsNull() && !spec.EndAt.IsUnknown() {
		t, err := time.Parse(time.RFC3339, spec.EndAt.ValueString())
		if err != nil {
			diags.AddError("Invalid end_at", err.Error())
			return result, diags
		}
		result.EndAt = t
	}

	return result, diags
}

func buildWorkflowAction(action *actionModel) (*client.ScheduleWorkflowAction, diag.Diagnostics) {
	var diags diag.Diagnostics
	if action == nil {
		diags.AddError("Missing action block", "An action block is required.")
		return nil, diags
	}

	wfAction := &client.ScheduleWorkflowAction{
		Workflow:  action.WorkflowType.ValueString(),
		TaskQueue: action.TaskQueue.ValueString(),
	}

	if !action.WorkflowID.IsNull() && !action.WorkflowID.IsUnknown() {
		wfAction.ID = action.WorkflowID.ValueString()
	}

	if !action.InputPayload.IsNull() && !action.InputPayload.IsUnknown() {
		var payload interface{}
		if err := json.Unmarshal([]byte(action.InputPayload.ValueString()), &payload); err != nil {
			diags.AddError("Invalid input_payload", fmt.Sprintf("Must be valid JSON: %s", err))
			return nil, diags
		}
		wfAction.Args = []interface{}{payload}
	}

	return wfAction, diags
}

func readScheduleIntoState(state *scheduleResourceModel, desc *client.ScheduleDescription) {
	if wfAction, ok := desc.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
		if state.Action == nil {
			state.Action = &actionModel{}
		}
		state.Action.WorkflowType = types.StringValue(wfAction.Workflow.(string))
		state.Action.TaskQueue = types.StringValue(wfAction.TaskQueue)
		if !state.Action.WorkflowID.IsNull() && wfAction.ID != "" {
			state.Action.WorkflowID = types.StringValue(wfAction.ID)
		}
	}

	if desc.Schedule.State != nil {
		state.IsPaused = types.BoolValue(desc.Schedule.State.Paused)
	}

	if desc.Schedule.Policy != nil {
		state.OverlapPolicy = types.StringValue(fromOverlapPolicy(desc.Schedule.Policy.Overlap))
	}
}

func toOverlapPolicy(s string) enumspb.ScheduleOverlapPolicy {
	switch s {
	case "skip":
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	case "buffer_one":
		return enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE
	case "buffer_all":
		return enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL
	case "cancel_other":
		return enumspb.SCHEDULE_OVERLAP_POLICY_CANCEL_OTHER
	case "terminate_other":
		return enumspb.SCHEDULE_OVERLAP_POLICY_TERMINATE_OTHER
	case "allow_all":
		return enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL
	default:
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	}
}

func fromOverlapPolicy(p enumspb.ScheduleOverlapPolicy) string {
	switch p {
	case enumspb.SCHEDULE_OVERLAP_POLICY_SKIP:
		return "skip"
	case enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE:
		return "buffer_one"
	case enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL:
		return "buffer_all"
	case enumspb.SCHEDULE_OVERLAP_POLICY_CANCEL_OTHER:
		return "cancel_other"
	case enumspb.SCHEDULE_OVERLAP_POLICY_TERMINATE_OTHER:
		return "terminate_other"
	case enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL:
		return "allow_all"
	default:
		return "skip"
	}
}

func parseRangeString(s string) []client.ScheduleRange {
	if s == "" || s == "*" {
		return nil
	}

	var ranges []client.ScheduleRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		r := client.ScheduleRange{}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			r.Start, _ = strconv.Atoi(strings.TrimSpace(bounds[0]))
			r.End, _ = strconv.Atoi(strings.TrimSpace(bounds[1]))
		} else {
			val, _ := strconv.Atoi(part)
			r.Start = val
			r.End = val
		}
		ranges = append(ranges, r)
	}
	return ranges
}
