// The axios instance and its interceptors, and the helpers every area uses.

import axios, { AxiosInstance, AxiosRequestConfig } from 'axios';
import { base } from './base';
import type { Progress } from './types/items';

const API_URL = base();

const client: AxiosInstance = axios.create({
  baseURL: API_URL,
  timeout: 1000, // one second
});

// Tag every request.
client.interceptors.request.use((config) => {
  config.headers['X-Fixture'] = '1';
  return config;
});

const uploadConfig = (onProgress?: Progress): AxiosRequestConfig => ({
  timeout: 0,
  onUploadProgress: onProgress ? (event) => onProgress(event.loaded) : undefined,
});

client.interceptors.response.use(
  (response) => response,
  (error) => Promise.reject(error),
);

// Hand the browser a file to save.
const save = (data: BlobPart, name: string): string => {
  const url = URL.createObjectURL(new Blob([data]));
  return `${url}#${name}`;
};

export { API_URL, client, uploadConfig, save };
