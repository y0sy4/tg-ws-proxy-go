name: Pull Request
description: Submit a pull request
title: "[PR] "
labels: ["enhancement"]
body:
  - type: markdown
    attributes:
      value: |
        Thanks for contributing to TG WS Proxy Go!
  - type: textarea
    id: description
    attributes:
      label: Description
      description: What does this PR do?
      placeholder: This PR adds/fixes...
    validations:
      required: true
  - type: textarea
    id: testing
    attributes:
      label: Testing
      description: How did you test this?
      placeholder: I tested on Windows/Linux/macOS...
    validations:
      required: true
  - type: checkboxes
    id: checklist
    attributes:
      label: Checklist
      options:
        - label: I have tested this locally
          required: true
        - label: Code follows project guidelines
          required: true
