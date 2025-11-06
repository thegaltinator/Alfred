"""
Cerebras API client for Alfred integration
Handles chat completion with memory context
"""

import json
import os
import sys
from typing import List, Dict, Any, Optional

try:
    import requests
except ImportError:
    print("❌ requests module not available. Install with: pip install requests", file=sys.stderr)
    requests = None


class CerebrasClient:
    """Client for Cerebras API integration"""

    def __init__(self, api_key: Optional[str] = None, base_url: str = "https://api.cerebras.ai/v1"):
        self.api_key = api_key or os.getenv('CEREBRAS_API_KEY')
        self.base_url = base_url.rstrip('/')

        if not self.api_key:
            print("⚠️ No Cerebras API key provided. Using mock responses.", file=sys.stderr)
            return

        if requests is None:
            print("❌ requests module not available. Cannot make API calls.", file=sys.stderr)
            return

        self.session = requests.Session()
        self.session.headers.update({
            'Authorization': f'Bearer {self.api_key}',
            'Content-Type': 'application/json'
        })

    def chat_with_memory(self, transcript: str, memories: List[Dict[str, Any]]) -> str:
        """
        Generate response using Cerebras with memory context

        Args:
            transcript: User's spoken transcript
            memories: List of relevant memories with similarity scores

        Returns:
            Generated response text
        """
        if not self.api_key or not requests:
            return self._mock_response(transcript, memories)

        try:
            # Build memory context
            memory_context = self._build_memory_context(memories)

            # Construct prompt
            system_prompt = self._build_system_prompt()
            user_prompt = self._build_user_prompt(transcript, memory_context)

            # Make API call
            response = self.session.post(
                f"{self.base_url}/chat/completions",
                json={
                    'model': 'llama3.1-70b',  # or appropriate Cerebras model
                    'messages': [
                        {'role': 'system', 'content': system_prompt},
                        {'role': 'user', 'content': user_prompt}
                    ],
                    'max_tokens': 300,  # Shorter responses for voice
                    'temperature': 0.7,
                    'top_p': 0.9,
                    'frequency_penalty': 0.5,
                    'presence_penalty': 0.3
                },
                timeout=10.0  # 10 second timeout for responsive UX
            )

            response.raise_for_status()
            result = response.json()

            return result['choices'][0]['message']['content'].strip()

        except requests.exceptions.Timeout:
            print("⚠️ Cerebras API timeout, using fallback", file=sys.stderr)
            return self._mock_response(transcript, memories)

        except requests.exceptions.RequestException as e:
            print(f"⚠️ Cerebras API error: {e}, using fallback", file=sys.stderr)
            return self._mock_response(transcript, memories)

        except (KeyError, IndexError, json.JSONDecodeError) as e:
            print(f"⚠️ Cerebras API response parsing error: {e}, using fallback", file=sys.stderr)
            return self._mock_response(transcript, memories)

        except Exception as e:
            print(f"⚠️ Unexpected error in Cerebras call: {e}, using fallback", file=sys.stderr)
            return self._mock_response(transcript, memories)

    def _build_memory_context(self, memories: List[Dict[str, Any]]) -> str:
        """Build formatted memory context string"""
        if not memories:
            return ""

        context_parts = ["Relevant memories:"]
        for i, memory in enumerate(memories, 1):
            content = memory['content'][:200]  # Limit length
            similarity = memory.get('similarity', 0)
            context_parts.append(f"{i}. {content} (similarity: {similarity:.2f})")

        return "\n".join(context_parts)

    def _build_system_prompt(self) -> str:
        """Build system prompt for Alfred"""
        return """You are Alfred, a helpful AI assistant with access to the user's memories.

Guidelines:
- Use the provided memories to give more personalized and contextual responses
- Be concise but helpful (responses should be speakable in 10-15 seconds)
- Reference relevant memories naturally in your responses
- If memories seem irrelevant, focus on answering the user's immediate question
- Maintain a friendly, helpful tone
- Avoid lengthy explanations unless specifically asked"""

    def _build_user_prompt(self, transcript: str, memory_context: str) -> str:
        """Build user prompt with transcript and memory context"""
        if memory_context:
            return f"""User said: {transcript}

{memory_context}

Please provide a helpful response that considers the relevant memories above."""
        else:
            return f"User said: {transcript}\n\nPlease provide a helpful response."

    def _mock_response(self, transcript: str, memories: List[Dict[str, Any]]) -> str:
        """Generate mock response when API is unavailable"""
        if memories:
            memory_count = len(memories)
            return f"I heard you say: {transcript}. I found {memory_count} relevant memory{'ies' if memory_count != 1 else ''} to help with that."
        else:
            return f"I heard you say: {transcript}. How can I help you with that?"

    def health_check(self) -> Dict[str, Any]:
        """
        Check if Cerebras API is accessible

        Returns:
            Health check result dictionary
        """
        if not self.api_key or not requests:
            return {
                'status': 'unavailable',
                'error': 'API key or requests module not available'
            }

        try:
            # Make a minimal API call to test connectivity
            response = self.session.post(
                f"{self.base_url}/chat/completions",
                json={
                    'model': 'llama3.1-70b',
                    'messages': [
                        {'role': 'user', 'content': 'Hello'}
                    ],
                    'max_tokens': 5
                },
                timeout=5.0
            )

            if response.status_code == 200:
                return {
                    'status': 'healthy',
                    'model': 'llama3.1-70b',
                    'latency_ms': response.elapsed.total_seconds() * 1000
                }
            else:
                return {
                    'status': 'error',
                    'error': f'HTTP {response.status_code}',
                    'message': response.text[:100]
                }

        except requests.exceptions.Timeout:
            return {
                'status': 'timeout',
                'error': 'Request timed out after 5 seconds'
            }

        except requests.exceptions.RequestException as e:
            return {
                'status': 'error',
                'error': str(e)
            }

        except Exception as e:
            return {
                'status': 'error',
                'error': f'Unexpected error: {str(e)}'
            }


# Standalone test function
def test_cerebras_client():
    """Test the Cerebras client functionality"""
    import os

    api_key = os.getenv('CEREBRAS_API_KEY')
    client = CerebrasClient(api_key)

    print("Testing Cerebras client...")

    # Test health check
    health = client.health_check()
    print(f"Health check: {health}")

    # Test with mock memories
    mock_memories = [
        {
            'content': 'User prefers working in the morning and likes coffee',
            'similarity': 0.85,
            'created_at': '2024-01-01T10:00:00Z'
        },
        {
            'content': 'User is learning Swift programming',
            'similarity': 0.72,
            'created_at': '2024-01-02T14:30:00Z'
        }
    ]

    test_transcript = "I'm having trouble with Swift concurrency"
    response = client.chat_with_memory(test_transcript, mock_memories)
    print(f"✅ Response for '{test_transcript}': {response[:100]}...")

    # Test without memories
    response_no_memories = client.chat_with_memory("What's the weather like?", [])
    print(f"✅ Response without memories: {response_no_memories[:100]}...")

    print("✅ Cerebras client test completed")


if __name__ == "__main__":
    test_cerebras_client()