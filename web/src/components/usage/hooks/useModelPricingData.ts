import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, deletePricingModel, fetchPricingModelOptions, fetchPricingModels, savePricingModel } from '@/lib/api';
import type { DeletePricingModelResponse, ModelPricingConfig, SavePricingModelResponse } from '@/lib/types';

export interface UseModelPricingDataOptions {
  enabled?: boolean;
  onAuthRequired?: () => void;
}

export interface ModelPricingMutationResult<T> {
  result: T;
  refreshed: boolean;
}

export interface UseModelPricingDataReturn {
  models: ModelPricingConfig[];
  modelOptions: string[];
  configRevision: number | null;
  loading: boolean;
  error: string;
  loadPricing: () => Promise<boolean>;
  saveModel: (config: ModelPricingConfig) => Promise<ModelPricingMutationResult<SavePricingModelResponse>>;
  deleteModel: (model: string) => Promise<ModelPricingMutationResult<DeletePricingModelResponse>>;
}

const readErrorMessage = (error: unknown): string =>
  error instanceof Error ? error.message : 'Failed to load pricing models';

// 管理员价格编辑页读取完整配置与候选模型，并把写入结果同随后列表刷新分开报告。
export function useModelPricingData(options: UseModelPricingDataOptions = {}): UseModelPricingDataReturn {
  const { enabled = true, onAuthRequired } = options;
  const [models, setModels] = useState<ModelPricingConfig[]>([]);
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [configRevision, setConfigRevision] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const requestRef = useRef<AbortController | null>(null);
  const mountedRef = useRef(false);
  const onAuthRequiredRef = useRef(onAuthRequired);

  useEffect(() => {
    onAuthRequiredRef.current = onAuthRequired;
  }, [onAuthRequired]);

  // 每次手动或写后刷新都取消旧读取；模型配置与修订只从同一次列表响应一起入状态。
  const loadPricing = useCallback(async (): Promise<boolean> => {
    if (!mountedRef.current) return false;
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    setLoading(true);
    setError('');

    try {
      const [modelResponse, optionsResponse] = await Promise.all([
        fetchPricingModels(controller.signal),
        fetchPricingModelOptions(controller.signal),
      ]);
      if (requestRef.current !== controller || controller.signal.aborted || !mountedRef.current) return false;
      setModels(modelResponse.models);
      setConfigRevision(modelResponse.config_revision);
      setModelOptions(optionsResponse.models);
      return true;
    } catch (readError) {
      if (requestRef.current !== controller || controller.signal.aborted || !mountedRef.current) return false;
      if (readError instanceof ApiError && readError.status === 401) onAuthRequiredRef.current?.();
      setError(readErrorMessage(readError));
      controller.abort();
      return false;
    } finally {
      if (requestRef.current === controller) {
        requestRef.current = null;
        if (mountedRef.current) setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    if (enabled) {
      void loadPricing();
    } else {
      setLoading(false);
    }
    return () => {
      mountedRef.current = false;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [enabled, loadPricing]);

  // 保存一次完整配置；后续读取失败不把已经提交的保存误报为写入失败。
  const saveModel = useCallback(async (config: ModelPricingConfig): Promise<ModelPricingMutationResult<SavePricingModelResponse>> => {
    let result: SavePricingModelResponse;
    try {
      result = await savePricingModel(config);
    } catch (writeError) {
      if (writeError instanceof ApiError && writeError.status === 401) onAuthRequiredRef.current?.();
      throw writeError;
    }
    return { result, refreshed: await loadPricing() };
  }, [loadPricing]);

  // 删除只移除当前配置，成功后从服务端重读列表和修订。
  const deleteModel = useCallback(async (model: string): Promise<ModelPricingMutationResult<DeletePricingModelResponse>> => {
    let result: DeletePricingModelResponse;
    try {
      result = await deletePricingModel(model);
    } catch (writeError) {
      if (writeError instanceof ApiError && writeError.status === 401) onAuthRequiredRef.current?.();
      throw writeError;
    }
    return { result, refreshed: await loadPricing() };
  }, [loadPricing]);

  return { models, modelOptions, configRevision, loading, error, loadPricing, saveModel, deleteModel };
}
