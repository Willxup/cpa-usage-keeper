/*!
 * Stream Output Regular from Microsoft Fluent UI System Icons.
 * https://github.com/microsoft/fluentui-system-icons
 *
 * MIT License
 * Copyright (c) 2020 Microsoft Corporation
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
import type { SVGProps } from 'react'

export function IconStreamOutput({ size = 20, ...props }: SVGProps<SVGSVGElement> & { size?: number }) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" width={size} height={size} fill="currentColor" {...props}>
      <path d="M16.1464 2.14645C16.3417 1.95118 16.6583 1.95118 16.8536 2.14645L19.8422 5.1351C19.9396 5.22633 20.0004 5.35606 20.0004 5.5C20.0004 5.64394 19.9396 5.77367 19.8422 5.8649L16.8536 8.85355C16.6583 9.04882 16.3417 9.04882 16.1464 8.85355C15.9512 8.65829 15.9512 8.34171 16.1464 8.14645L18.2929 6H13.5C11.6 6 10 7.6 10 9.5V10.5C10 13 8 15 5.5 15H2.5C2.2 15 2 14.8 2 14.5C2 14.2 2.2 14 2.5 14H5.5C7.4 14 9 12.4 9 10.5V9.5C9 7 11 5 13.5 5H18.2929L16.1464 2.85355C15.9512 2.65829 15.9512 2.34171 16.1464 2.14645ZM2.5 12H5C6.1 12 7 11.1 7 10V9C7 5.7 9.7 3 13 3H13.5C13.8 3 14 2.8 14 2.5C14 2.2 13.8 2 13.5 2H13C9.1 2 6 5.1 6 9V10C6 10.6 5.6 11 5 11H2.5C2.2 11 2 11.2 2 11.5C2 11.8 2.2 12 2.5 12ZM15.1 8H14C12.9 8 12 8.9 12 10V11C12 14.3 9.3 17 6 17H5.5C5.2 17 5 17.2 5 17.5C5 17.8 5.2 18 5.5 18H6C9.9 18 13 14.9 13 11V10C13 9.4 13.4 9 14 9H15.1C15 8.7 15 8.3 15.1 8Z" />
    </svg>
  )
}
