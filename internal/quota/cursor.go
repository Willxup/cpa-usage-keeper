package quota

import (
	"context"
	"fmt"
	"sync"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

const cursorConnectUnaryBody = "{}"

type cursorProvider struct {
	caller       ManagementAPICaller
	planConfig   APICallConfig
	periodConfig APICallConfig
	agentConfig  APICallConfig
}

func NewCursorProvider(caller ManagementAPICaller, planConfig APICallConfig, periodConfig APICallConfig, agentConfig APICallConfig) ProviderHandler {
	return cursorProvider{caller: caller, planConfig: planConfig, periodConfig: periodConfig, agentConfig: agentConfig}
}

func (p cursorProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	if p.periodConfig.URL == "" {
		return ProviderOutput{}, fmt.Errorf("%w: cursor period config is required", ErrProviderInput)
	}

	var (
		period    *CursorPeriodPayload
		plan      *CursorPlanPayload
		agent     *CursorAgentPayload
		periodErr error
		wg        sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		period, periodErr = p.requestPeriod(ctx, input)
	}()
	if p.planConfig.URL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plan = p.requestPlan(ctx, input)
		}()
	}
	if p.agentConfig.URL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent = p.requestAgent(ctx, input)
		}()
	}
	wg.Wait()
	if periodErr != nil {
		return ProviderOutput{}, periodErr
	}
	return ProviderOutput{Provider: "cursor", Result: CursorResult{Plan: plan, Period: period, Agent: agent}}, nil
}

func (p cursorProvider) requestPeriod(ctx context.Context, input ProviderInput) (*CursorPeriodPayload, error) {
	response, err := p.call(ctx, input, p.periodConfig)
	if err != nil {
		return nil, err
	}
	return parseCursorPeriodPayload(response)
}

func (p cursorProvider) requestPlan(ctx context.Context, input ProviderInput) *CursorPlanPayload {
	response, err := p.call(ctx, input, p.planConfig)
	if err != nil {
		return nil
	}
	plan, err := parseCursorPlanPayload(response)
	if err != nil {
		return nil
	}
	return plan
}

func (p cursorProvider) requestAgent(ctx context.Context, input ProviderInput) *CursorAgentPayload {
	response, err := p.call(ctx, input, p.agentConfig)
	if err != nil {
		return nil
	}
	agent, err := parseCursorAgentPayload(response)
	if err != nil {
		return nil
	}
	return agent
}

func (p cursorProvider) call(ctx context.Context, input ProviderInput, config APICallConfig) (*apicall.Response, error) {
	return p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    config.Method,
		URL:       config.URL,
		Header:    copyHeaders(config.Headers),
		Data:      cursorConnectUnaryBody,
	})
}
