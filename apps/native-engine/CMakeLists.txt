cmake_minimum_required(VERSION 3.20)
project(rpd_native_engine LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_executable(rpd-native-engine
    src/main.cpp
    src/image_processor.cpp
)
target_include_directories(rpd-native-engine PRIVATE include)

if (MSVC)
  target_compile_options(rpd-native-engine PRIVATE /W4)
else()
  target_compile_options(rpd-native-engine PRIVATE -Wall -Wextra -Wpedantic)
endif()
